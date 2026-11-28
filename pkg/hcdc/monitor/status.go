package monitor

import (
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
)

// StateNotStarted 表示已配置但尚未启动的源。
const StateNotStarted hcdc.PipelineState = "not_started"

// Action 表示当前源可执行的常用控制操作。
type Action string

const (
	ActionStart  Action = "start"
	ActionPause  Action = "pause"
	ActionResume Action = "resume"
	ActionStop   Action = "stop"
)

// Snapshot 包含本次读取的源状态及取样完成时间。
type Snapshot struct {
	Sources   []SourceStatus `json:"sources"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// SourceStatus 包含单源的状态、进度和当前可执行操作。
// Go 计数使用 uint64，JSON 中对应字段为十进制字符串。
type SourceStatus struct {
	ID                   string             `json:"id"`
	Generation           string             `json:"generation"`
	TargetDatabase       string             `json:"targetDatabase"`
	State                hcdc.PipelineState `json:"state"`
	Since                *time.Time         `json:"since,omitempty"`
	Reason               string             `json:"reason,omitempty"`
	Error                string             `json:"error,omitempty"`
	DeliveryError        string             `json:"deliveryError,omitempty"`
	CaptureError         string             `json:"captureError,omitempty"`
	PendingBytes         int64              `json:"pendingBytes"`
	Backpressured        bool               `json:"backpressured"`
	InFlight             bool               `json:"inFlight"`
	ReadCursor           Cursor             `json:"readCursor"`
	Durable              Checkpoint         `json:"durable"`
	Delivered            Checkpoint         `json:"delivered"`
	SnapshotZeroDateRows uint64             `json:"snapshotZeroDateRows,string"`
	PendingDDL           *DDLStatus         `json:"pendingDDL,omitempty"`
	Actions              []Action           `json:"actions"`
	DiscoveredTables     []TableStatus      `json:"discoveredTables,omitempty"`
	TableDiscoveryError  string             `json:"tableDiscoveryError,omitempty"`
}

// TableStatus 报告自动接入表的独立快照、增量位点和待审批结构变更。
type TableStatus struct {
	Name                 string             `json:"name"`
	State                hcdc.PipelineState `json:"state"`
	Error                string             `json:"error,omitempty"`
	CaptureError         string             `json:"captureError,omitempty"`
	DeliveryError        string             `json:"deliveryError,omitempty"`
	Durable              Checkpoint         `json:"durable"`
	Delivered            Checkpoint         `json:"delivered"`
	PendingBytes         int64              `json:"pendingBytes"`
	Backpressured        bool               `json:"backpressured"`
	SnapshotZeroDateRows uint64             `json:"snapshotZeroDateRows,string"`
	PendingDDL           *DDLStatus         `json:"pendingDDL,omitempty"`
}

// Cursor 描述物理 Binlog 位置及其 GTID 集合。
type Cursor struct {
	File     string `json:"file"`
	Position uint32 `json:"position"`
	GTIDSet  string `json:"gtidSet"`
}

// Checkpoint 描述已落盘或已交付的有序记录及其累计映射行数。
type Checkpoint struct {
	Sequence     uint64 `json:"sequence,string"`
	Cursor       Cursor `json:"cursor"`
	ZeroDateRows uint64 `json:"zeroDateRows,string"`
}

// DDLStatus 描述等待交付的 DDL。通过 ID 审批当前允许执行的操作。
type DDLStatus struct {
	ID               string       `json:"id"`
	Kind             hcdc.DDLKind `json:"kind"`
	Table            string       `json:"table"`
	Reason           string       `json:"reason,omitempty"`
	RequiresApproval bool         `json:"requiresApproval"`
}

// pipelineStatus 返回句柄当前的状态以及资源是否已释放。
func pipelineStatus(p *hcdc.Pipeline) (hcdc.PipelineStatus, bool) {
	if p == nil {
		return hcdc.PipelineStatus{State: StateNotStarted}, true
	}
	state := p.Status()
	if pipelineDone(p) {
		// Done 可能在第一次取样之后关闭，此时重新读取已发布的终态。
		return p.Status(), true
	}
	return state, false
}

// status 从当前句柄构造单源状态。
func (s *source) status(p *hcdc.Pipeline, closed bool) SourceStatus {
	state, done := pipelineStatus(p)
	return s.snapshot(state, done, closed)
}

// snapshot 投影同步进度，遮盖配置中已知的凭据。
func (s *source) snapshot(state hcdc.PipelineStatus, done, closed bool) SourceStatus {
	identity := s.resolved.Identity()
	output := SourceStatus{
		ID: identity.SourceID, Generation: identity.Generation, TargetDatabase: s.resolved.TargetDatabase(), State: state.State,
		Reason: s.resolved.RedactMessage(state.Reason), DeliveryError: s.resolved.RedactMessage(state.DeliveryError),
		CaptureError: s.resolved.RedactMessage(state.CaptureError),
		PendingBytes: state.Progress.PendingBytes, Backpressured: state.Backpressured, InFlight: state.InFlight,
		ReadCursor: snapshotCursor(state.ReadCursor), Durable: snapshotCheckpoint(state.Progress.Durable),
		Delivered: snapshotCheckpoint(state.Progress.Delivered), SnapshotZeroDateRows: state.SnapshotZeroDateRows,
		Actions:             availableActions(state.State, done, closed),
		TableDiscoveryError: s.resolved.RedactMessage(state.TableDiscoveryError),
	}
	if !state.Since.IsZero() {
		since := state.Since.UTC()
		output.Since = &since
	}
	if state.Err != nil {
		output.Error = s.resolved.RedactMessage(state.Err.Error())
	}
	output.PendingDDL = s.ddlStatus(state.PendingDDL)
	for _, table := range state.DiscoveredTables {
		status := table.Status
		item := TableStatus{
			Name: table.Name, State: status.State,
			CaptureError: s.resolved.RedactMessage(status.CaptureError), DeliveryError: s.resolved.RedactMessage(status.DeliveryError),
			Durable: snapshotCheckpoint(status.Progress.Durable), Delivered: snapshotCheckpoint(status.Progress.Delivered),
			PendingBytes: status.Progress.PendingBytes, Backpressured: status.Backpressured,
			SnapshotZeroDateRows: status.SnapshotZeroDateRows, PendingDDL: s.ddlStatus(status.PendingDDL),
		}
		if status.Err != nil {
			item.Error = s.resolved.RedactMessage(status.Err.Error())
		}
		output.DiscoveredTables = append(output.DiscoveredTables, item)
	}
	return output
}

// ddlStatus 将待审批计划转成监控状态并过滤连接凭据。
func (s *source) ddlStatus(plan *hcdc.DDLPlan) *DDLStatus {
	if plan == nil {
		return nil
	}
	return &DDLStatus{
		ID: plan.ID, Kind: plan.Kind, Table: plan.Before.Database + "." + plan.Before.Name,
		Reason: s.resolved.RedactMessage(plan.Reason), RequiresApproval: plan.RequiresApproval,
	}
}

// availableActions 按状态和资源释放情况列出可执行操作。
func availableActions(state hcdc.PipelineState, done, closed bool) []Action {
	if !closed {
		if done {
			switch state {
			case StateNotStarted, hcdc.PipelineStopped, hcdc.PipelineFailed:
				return []Action{ActionStart}
			}
		} else {
			switch state {
			case hcdc.PipelineStarting, hcdc.PipelineAwaitingApproval:
				return []Action{ActionStop}
			case hcdc.PipelineRunning:
				return []Action{ActionPause, ActionStop}
			case hcdc.PipelinePaused:
				return []Action{ActionResume, ActionStop}
			}
		}
	}
	return []Action{}
}

// snapshotCursor 提取源日志位置，省略成员和拓扑配置。
func snapshotCursor(value hcdc.Cursor) Cursor {
	return Cursor{File: value.File, Position: value.Position, GTIDSet: value.GTIDSet}
}

// snapshotCheckpoint 提取监控所需的检查点字段。
func snapshotCheckpoint(value hcdc.Checkpoint) Checkpoint {
	return Checkpoint{Sequence: value.Sequence, Cursor: snapshotCursor(value.Cursor), ZeroDateRows: value.ZeroDateRows}
}
