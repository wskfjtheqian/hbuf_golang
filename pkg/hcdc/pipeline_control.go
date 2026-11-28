package hcdc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// PipelineState 表示当前运行阶段或阻塞交付的状态。
type PipelineState string

const (
	PipelineStarting         PipelineState = "starting"
	PipelineRunning          PipelineState = "running"
	PipelinePaused           PipelineState = "paused"
	PipelineAwaitingApproval PipelineState = "awaiting_approval"
	PipelineStopping         PipelineState = "stopping"
	PipelineStopped          PipelineState = "stopped"
	PipelineFailed           PipelineState = "failed"
)

// ErrControlOutcomeUnknown 表示控制命令仍在落盘，或已发布记录的持久性尚未确认。
// 交付等待该记录得到确认。
var ErrControlOutcomeUnknown = herror.NewError("CDC control journal durability is uncertain")

// PipelineStatus 是源端采集与目标交付进度在某一时刻的快照。
type PipelineStatus struct {
	Identity StreamIdentity
	State    PipelineState
	Progress DeliveryStatus
	// SnapshotZeroDateRows 为已确认的全量快照中映射了零日期的行数。
	SnapshotZeroDateRows uint64
	ReadCursor           Cursor
	PendingDDL           *DDLPlan
	InFlight             bool
	Backpressured        bool // 采集持有完整记录，正在等待缓冲空间。
	Since                time.Time
	Reason               string
	CaptureError         string // 采集重连前的源端传输错误。
	DeliveryError        string // 当前队首记录触发重试的原因。
	Err                  error
	// DiscoveredTables 独立报告自动接入表的进度，不并入父流位点。
	DiscoveredTables    []TableSyncStatus
	TableDiscoveryError string
}

// TableSyncStatus 保存自动接入表的同步状态。
type TableSyncStatus struct {
	Name   string
	Status PipelineStatus
}

// Pipeline 管理单个源的同步任务及其连接生命周期。
type Pipeline struct {
	mu            sync.Mutex
	changed       chan struct{}
	statusChanged chan struct{}
	done          chan struct{}
	cancel        context.CancelFunc
	stopCapture   context.CancelFunc

	status          PipelineStatus
	ready           bool
	stopping        bool
	control         interface{ Append(uint64, []byte) error }
	controlSequence uint64
	controlWrite    *controlWrite
	pendingControl  *controlRecord
	pausedReason    string
	approved        map[string]bool
	result          error
	tableParent     *Pipeline
	tableStreams    map[string]tableStream
	tableWrites     int
	tableWritesDone chan struct{}
}

type controlAction string

const (
	controlPause   controlAction = "pause"
	controlResume  controlAction = "resume"
	controlApprove controlAction = "approve_ddl"
)

type controlRecord struct {
	Action controlAction
	Reason string
	DDLID  string
}

type controlWrite struct {
	done chan struct{}
	err  error
}

type pipelineStartOptions struct {
	resumeOnly          bool
	activate            <-chan struct{}
	tableOwner          *dorisWriterIdentity
	tableParent         *Pipeline
	tableSourceIdentity string
}

// startPipeline 校验配置并在后台启动全量快照或断点恢复。
// 可通过 Status 或 Wait 查看异步错误。
func startPipeline(ctx context.Context, config pipelineConfig) (*Pipeline, error) {
	return startPipelineWithOptions(ctx, config, pipelineStartOptions{})
}

// startTablePipeline 使用库流水线的 Doris 写入凭证启动单表同步。
func startTablePipeline(ctx context.Context, config pipelineConfig, parentOwner dorisWriterIdentity, parent *Pipeline, sourceIdentity string) (*Pipeline, error) {
	return startPipelineWithOptions(ctx, config, pipelineStartOptions{
		tableOwner: &parentOwner, tableParent: parent, tableSourceIdentity: sourceIdentity,
	})
}

// startPipelineWithOptions 获取单源所有权，按启动选项运行任务并发布终止结果。
func startPipelineWithOptions(ctx context.Context, config pipelineConfig, options pipelineStartOptions) (*Pipeline, error) {
	if err := ctx.Err(); err != nil {
		return nil, herror.Wrap(err)
	}
	if _, _, err := validatePipelineConfig(config); err != nil {
		return nil, err
	}
	release, err := acquirePipelineOwnership(config, options.tableOwner)
	if err != nil {
		return nil, err
	}
	config.Source.Tables = append([]string(nil), config.Source.Tables...)
	if hlog.FromContext(ctx) == "" {
		ctx = hlog.WithContext(ctx, hlog.NewTraceId())
	}
	runCtx, cancel := context.WithCancel(ctx)
	captureCtx, stopCapture := context.WithCancel(runCtx)
	pipeline := &Pipeline{
		changed: make(chan struct{}), done: make(chan struct{}),
		cancel: cancel, stopCapture: stopCapture, approved: make(map[string]bool),
		status:      PipelineStatus{Identity: config.Identity, State: PipelineStarting, Since: time.Now()},
		tableParent: options.tableParent,
	}
	go func() {
		err := pipeline.run(runCtx, captureCtx, config, options)
		cancelled := runCtx.Err() != nil
		stopCapture()
		cancel()
		release()

		pipeline.mu.Lock()
		pipeline.result = err
		pipeline.status.CaptureError = ""
		pipeline.status.DeliveryError = ""
		pipeline.status.Backpressured = false
		switch {
		case err == nil || (cancelled && contextErrorOnly(err)):
			pipeline.transitionLocked(PipelineStopped, "")
		default:
			pipeline.status.Err = err
			pipeline.transitionLocked(PipelineFailed, err.Error())
		}
		pipeline.signalLocked()
		pipeline.mu.Unlock()
		close(pipeline.done)
	}()
	return pipeline, nil
}

// Status 返回流水线当前进度和待处理 DDL 计划的独立副本。
func (p *Pipeline) Status() PipelineStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statusLocked()
}

// statusLocked 在持锁时复制快照，隔离调用方对待审批计划的修改。
func (p *Pipeline) statusLocked() PipelineStatus {
	status := p.status
	status.DiscoveredTables = p.tableStatusesLocked()
	if status.PendingDDL != nil {
		plan := *status.PendingDDL
		plan.Before.Columns = append([]DorisColumn(nil), plan.Before.Columns...)
		plan.Before.PrimaryKey = append([]string(nil), plan.Before.PrimaryKey...)
		plan.After.Columns = append([]DorisColumn(nil), plan.After.Columns...)
		plan.After.PrimaryKey = append([]string(nil), plan.After.PrimaryKey...)
		status.PendingDDL = &plan
	}
	return status
}

// WatchStatus 立即报告当前状态，随后在调用方的 goroutine 内串行回调。
// 回调执行期间的变更会合并到最新快照。回调在锁外执行，可以调用 Status 或控制方法。
// 发送 stopped 或 failed 后返回 nil，流水线错误由快照的 Err 提供。
// 监听取消后返回上下文错误，已开始的回调可通过传入的上下文结束自己。
func (p *Pipeline) WatchStatus(ctx context.Context, callback func(context.Context, PipelineStatus)) error {
	if callback == nil {
		return herror.NewError("CDC status callback is required")
	}
	for {
		if err := ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
		p.mu.Lock()
		status := p.statusLocked()
		terminal := status.State == PipelineStopped || status.State == PipelineFailed
		if p.statusChanged == nil && !terminal {
			p.statusChanged = make(chan struct{})
		}
		changed := p.statusChanged
		p.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
		callback(ctx, status)
		if terminal {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return herror.Wrap(ctx.Err())
		}
	}
}

// Done 返回完成通道，采集与交付停止且资源释放后该通道关闭。
func (p *Pipeline) Done() <-chan struct{} { return p.done }

// Wait 等待流水线释放资源并返回终止错误。
func (p *Pipeline) Wait() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result
}

// waitReady 等待流水线完成订阅校验，并返回等待期限或启动错误。
func (p *Pipeline) waitReady(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
		p.mu.Lock()
		ready, changed := p.ready && !p.stopping, p.changed
		p.mu.Unlock()
		if ready {
			if err := ctx.Err(); err != nil {
				return herror.Wrap(err)
			}
			return nil
		}
		select {
		case <-changed:
		case <-p.done:
			if err := p.Wait(); err != nil {
				return err
			}
			return herror.NewError("CDC pipeline stopped before becoming ready")
		case <-ctx.Done():
			return herror.Wrap(ctx.Err())
		}
	}
}

// Pause 持久化手动暂停，等待在途请求达到已知或可恢复状态。
// 源端采集会继续，直到缓冲达到上限。
func (p *Pipeline) Pause(ctx context.Context, reason string) error {
	if reason == "" {
		reason = "manual pause"
	}
	if err := p.updateControl(ctx, controlRecord{Action: controlPause, Reason: reason}); err != nil {
		return err
	}
	p.mu.Lock()
	for p.status.InFlight || p.tableWrites != 0 {
		if p.tableWrites != 0 && p.tableWritesDone == nil {
			p.tableWritesDone = make(chan struct{})
		}
		changed, tableWritesDone := p.changed, p.tableWritesDone
		p.mu.Unlock()
		select {
		case <-changed:
		case <-tableWritesDone:
		case <-ctx.Done():
			return herror.Wrap(ctx.Err())
		}
		p.mu.Lock()
	}
	p.mu.Unlock()
	return nil
}

// Resume 清除持久化的手动暂停，DDL 屏障仍由审批控制。
func (p *Pipeline) Resume(ctx context.Context) error {
	return p.updateControl(ctx, controlRecord{Action: controlResume})
}

// ApproveDDL 持久化对队首可执行 DDL 屏障的审批。手动暂停仍由 Resume 控制。
func (p *Pipeline) ApproveDDL(ctx context.Context, id string) error {
	if handled, err := p.approveTableDDL(ctx, id); handled {
		return err
	}
	return p.updateControl(ctx, controlRecord{Action: controlApprove, DDLID: id})
}

// Close 停止采集和新派发，并等待在途交付完成。超时会取消在途交付并保留恢复记录。
func (p *Pipeline) Close(ctx context.Context) error {
	p.requestStop()
	select {
	case <-p.done:
		return p.closeResult()
	default:
	}
	select {
	case <-p.done:
		return p.closeResult()
	case <-ctx.Done():
		select {
		case <-p.done:
			return p.closeResult()
		default:
			p.cancel()
			return herror.Wrap(ctx.Err())
		}
	}
}

// requestStop 发出停止采集和新派发的信号，并允许已就绪流水线完成在途交付。
func (p *Pipeline) requestStop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stopping && p.status.State != PipelineStopped && p.status.State != PipelineFailed {
		p.stopping = true
		p.transitionLocked(PipelineStopping, "")
		p.signalLocked()
		p.stopCapture()
		if !p.ready {
			p.cancel()
		}
	}
}

// closeResult 返回关闭结果，将正常停止时的上下文取消视为成功。
func (p *Pipeline) closeResult() error {
	err := p.Wait()
	if p.Status().State == PipelineStopped && contextErrorOnly(err) {
		return nil
	}
	return err
}

// updateControl 串行提交控制记录。结果不明的记录使用相同序号和内容核对成功后才接受新记录。
func (p *Pipeline) updateControl(ctx context.Context, requested controlRecord) error {
	for {
		p.mu.Lock()
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return herror.Wrap(err)
		}
		if !p.ready || p.stopping || p.status.State == PipelineFailed || p.status.State == PipelineStopped {
			p.mu.Unlock()
			return herror.NewError("CDC pipeline is not accepting control changes")
		}
		if active := p.controlWrite; active != nil {
			p.mu.Unlock()
			select {
			case <-active.done:
				continue
			case <-ctx.Done():
				return herror.Wrap(ctx.Err())
			}
		}
		record := requested
		recovering := p.pendingControl != nil
		if recovering {
			record = *p.pendingControl
		} else if required, err := p.controlRequiredLocked(record); !required || err != nil {
			p.mu.Unlock()
			return err
		}
		write := &controlWrite{done: make(chan struct{})}
		p.controlWrite = write
		sequence := p.controlSequence + 1
		p.publishStateLocked()
		p.signalLocked()
		p.mu.Unlock()

		go p.persistControl(write, sequence, record)
		select {
		case <-write.done:
		case <-ctx.Done():
			select {
			case <-write.done:
			default:
				return errors.Join(ErrControlOutcomeUnknown, herror.Wrap(ctx.Err()))
			}
		}
		if write.err != nil || !recovering || record == requested {
			return write.err
		}
	}
}

// controlRequiredLocked 检查控制命令的前置条件以及相同命令是否已经生效。
func (p *Pipeline) controlRequiredLocked(record controlRecord) (bool, error) {
	switch record.Action {
	case controlPause:
		return p.pausedReason != record.Reason, nil
	case controlResume:
		return p.pausedReason != "", nil
	case controlApprove:
		plan := p.status.PendingDDL
		if plan == nil || plan.ID != record.DDLID {
			return false, herror.NewError("CDC approval does not match an executable pending DDL barrier")
		}
		if plan.Kind == DDLUnsupported {
			return false, herror.NewError("CDC DDL requires a new generation: " + plan.Reason)
		}
		if !plan.RequiresApproval {
			return false, herror.NewError("CDC pending DDL does not require approval")
		}
		return !p.approved[record.DDLID], nil
	default:
		return false, herror.NewError("invalid CDC pipeline control action")
	}
}

// persistControl 落盘时释放状态锁，仅在持久确认后提交内存状态。
func (p *Pipeline) persistControl(write *controlWrite, sequence uint64, record controlRecord) {
	encoded, err := json.Marshal(record)
	if err == nil {
		err = p.control.Append(sequence, encoded)
	} else {
		err = herror.Wrap(err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		p.controlSequence = sequence
		p.pendingControl = nil
		p.applyControlLocked(record)
	} else if errors.Is(err, spool.ErrPublicationUncertain) {
		p.pendingControl = &record
	}
	if p.pendingControl != nil {
		err = errors.Join(ErrControlOutcomeUnknown, err)
	}
	p.controlWrite = nil
	p.publishStateLocked()
	p.signalLocked()
	write.err = err
	close(write.done)
}

// waitForControlWrite 停止接纳新命令，等已开始的落盘结束后才允许释放控制目录。
func (p *Pipeline) waitForControlWrite() {
	p.mu.Lock()
	p.ready = false
	active := p.controlWrite
	p.signalLocked()
	p.mu.Unlock()
	if active != nil {
		<-active.done
	}
}

// applyControlLocked 将已持久化的暂停、恢复或审批记录应用到内存状态。
func (p *Pipeline) applyControlLocked(record controlRecord) {
	switch record.Action {
	case controlPause:
		p.pausedReason = record.Reason
	case controlResume:
		p.pausedReason = ""
	case controlApprove:
		p.approved[record.DDLID] = true
	}
}

// transitionLocked 在持锁时更新运行阶段、状态原因和变更时间。
func (p *Pipeline) transitionLocked(state PipelineState, reason string) {
	if p.status.State != state || p.status.Reason != reason {
		p.status.State, p.status.Reason, p.status.Since = state, reason, time.Now()
	}
}

// publishStateLocked 按停止、待确认控制、审批、暂停和就绪的优先级发布状态。
func (p *Pipeline) publishStateLocked() {
	switch {
	case p.stopping:
		p.transitionLocked(PipelineStopping, "")
	case p.pendingControl != nil:
		p.transitionLocked(PipelinePaused, ErrControlOutcomeUnknown.Error())
	case p.controlWrite != nil:
		p.transitionLocked(PipelinePaused, "persisting CDC control command")
	case p.status.PendingDDL != nil && p.status.PendingDDL.Kind == DDLUnsupported:
		p.transitionLocked(PipelineAwaitingApproval, "new generation required: "+p.status.PendingDDL.Reason)
	case p.status.PendingDDL != nil && p.status.PendingDDL.RequiresApproval && !p.approved[p.status.PendingDDL.ID]:
		reason := p.status.PendingDDL.Reason
		if reason == "" {
			reason = "DDL approval required"
		}
		p.transitionLocked(PipelineAwaitingApproval, reason)
	case p.pausedReason != "":
		p.transitionLocked(PipelinePaused, p.pausedReason)
	case p.ready:
		p.transitionLocked(PipelineRunning, "")
	}
}

// signalLocked 通知所有状态等待者，并准备下一轮通知通道。
func (p *Pipeline) signalLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
	p.signalStatusLocked()
}

// signalStatusLocked 唤醒状态监听者，不唤醒等待缓冲或控制命令的工作循环。
func (p *Pipeline) signalStatusLocked() {
	if p.statusChanged != nil {
		close(p.statusChanged)
		p.statusChanged = nil
	}
}

// isApproved 查询指定 DDL 屏障的审批记录。
func (p *Pipeline) isApproved(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.approved[id]
}

// isStopping 判断流水线是否已收到停止请求。
func (p *Pipeline) isStopping() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopping
}

// contextErrorOnly 判断错误的全部原因是否均为上下文取消或超时。
func contextErrorOnly(err error) bool {
	if err == nil {
		return false
	}
	if causes, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range causes.Unwrap() {
			if !contextErrorOnly(cause) {
				return false
			}
		}
		return true
	}
	if cause := errors.Unwrap(err); cause != nil {
		return contextErrorOnly(cause)
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
