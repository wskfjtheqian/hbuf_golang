package hcdc

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// restoreControl 按序读回持久化的暂停、恢复和 DDL 审批记录。
func (p *Pipeline) restoreControl(ctx context.Context, store *spool.Store, identity StreamIdentity) error {
	identityPayload, err := json.Marshal(identity)
	if err != nil {
		return herror.Wrap(err)
	}
	if _, err := store.Bind(identityPayload); err != nil {
		return err
	}
	sequences, err := store.Sequences()
	if err != nil {
		return err
	}
	for index, sequence := range sequences {
		if err := ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
		if sequence != uint64(index)+1 {
			return herror.NewError("CDC control journal contains a sequence gap")
		}
		data, err := store.Read(sequence)
		if err != nil {
			return err
		}
		var record controlRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return herror.WrapMsg(err, "decode CDC pipeline control record")
		}
		switch {
		case record.Action == controlPause && record.Reason != "",
			record.Action == controlResume,
			record.Action == controlApprove && record.DDLID != "":
			p.mu.Lock()
			p.applyControlLocked(record)
			p.controlSequence = sequence
			p.mu.Unlock()
		default:
			return herror.NewError("invalid CDC pipeline control record")
		}
	}
	p.mu.Lock()
	p.control = store
	p.mu.Unlock()
	return nil
}

// runWorkers 协调采集与交付，等订阅校验和配置启用完成后开放新记录处理。
func (p *Pipeline) runWorkers(ctx, captureCtx context.Context, source *mysqlsource.Source, delivery *Delivery, history *spool.Store, version schemaVersion, revision string, config PipelineConfig, activate <-chan struct{}) error {
	delivery.ddlApproved = p.isApproved
	progress := delivery.Status()
	p.mu.Lock()
	p.status.Progress = progress
	p.status.ReadCursor = progress.Durable.Cursor
	p.signalLocked()
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.ready = false
		p.signalLocked()
		p.mu.Unlock()
	}()

	verified := make(chan struct{})
	markReady := func() {
		p.mu.Lock()
		p.ready = true
		p.publishStateLocked()
		p.signalLocked()
		p.mu.Unlock()
		// 候选已通过订阅校验。等管理器确认后，采集和目标派发才可推进持久进度。
		if activate != nil {
			select {
			case <-activate:
			case <-captureCtx.Done():
				return
			}
		}
		close(verified)
	}
	hlog.Info(ctx, "CDC incremental subscription source_id=%s sequence=%d gtid_auto_position=%t", config.Identity.SourceID, progress.Delivered.Sequence, config.Source.GTIDAutoPosition)
	deliverCtx, cancelDelivery := context.WithCancel(ctx)
	defer cancelDelivery()
	captured, delivered := make(chan error, 1), make(chan error, 1)
	go func() {
		captured <- p.capture(captureCtx, source, delivery, history, version, revision, config, markReady)
	}()
	go func() {
		select {
		case <-verified:
			delivered <- p.deliver(deliverCtx, delivery, history)
		case <-captureCtx.Done():
			delivered <- nil
		case <-deliverCtx.Done():
			delivered <- herror.Wrap(deliverCtx.Err())
		}
	}()
	var captureErr, deliveryErr error
	select {
	case captureErr = <-captured:
		stopping := p.isStopping()
		if !stopping {
			cancelDelivery()
		}
		deliveryErr = <-delivered
		if !stopping && contextErrorOnly(deliveryErr) {
			deliveryErr = nil
		}
		if stopping && captureCtx.Err() != nil && contextErrorOnly(captureErr) {
			captureErr = nil
		}
	case deliveryErr = <-delivered:
		p.stopCapture()
		captureErr = <-captured
		if contextErrorOnly(captureErr) {
			captureErr = nil
		}
	}
	return errors.Join(captureErr, deliveryErr)
}

// capture 从已落盘位点续订，将提交事务或 DDL 屏障依次写入有容量限制的缓冲。
func (p *Pipeline) capture(ctx context.Context, source *mysqlsource.Source, delivery *Delivery, history *spool.Store, version schemaVersion, revision string, config PipelineConfig, ready func()) error {
	cursor := mysqlsource.Cursor(delivery.Status().Durable.Cursor)
	if version.Blocked {
		return source.StreamWithReady(ctx, cursor, version.Tables, func() {
			ready()
			<-ctx.Done()
		}, func(mysqlsource.Transaction) error { return herror.Wrap(ctx.Err()) })
	}
	layouts, err := buildTableLayouts(config, version.Tables)
	if err != nil {
		return err
	}
	config = withPipelineConfigDefaults(config)
	limit := config.MaxPendingBytes
	planner := newTransactionPlan(ctx, layouts, config.MaxTransactionBytes, delivery.store.CreateTemporaryFile)
	defer planner.reset()
	consume := func(transaction mysqlsource.Transaction) error {
		defer planner.reset()
		if err := ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
		p.mu.Lock()
		if cursor := Cursor(transaction.Cursor); p.status.ReadCursor != cursor {
			p.status.ReadCursor = cursor
			p.signalStatusLocked()
		}
		p.mu.Unlock()

		durable := delivery.Status().Durable
		record := OrderedRecord{
			Sequence: durable.Sequence + 1, TransactionID: transaction.ID,
			Cursor: Cursor(transaction.Cursor), StructureRevision: revision, ZeroDateRows: durable.ZeroDateRows,
		}
		var staged *stagedRecord
		if transaction.DDL != nil {
			next, plan, payload, err := planSchemaChange(config.TargetDatabase, config.Identity, version, record.Sequence, transaction)
			if err != nil {
				return err
			}
			layouts, err = buildTableLayouts(config, next.Tables)
			if err != nil {
				return err
			}
			if err := history.Append(next.Sequence, payload); err != nil {
				return err
			}
			version, revision = next, digest(payload)
			record.StructureRevision = revision
			record.DDL = &plan
			planner.setLayouts(layouts)
		} else if !transaction.RolledBack {
			if len(planner.runs) > 0 {
				var err error
				staged, err = delivery.stageRecord(record, limit, planner.batches)
				if err != nil {
					return err
				}
				defer staged.close()
			} else {
				mapped, err := planner.batches(func(batch LoadBatch) error {
					record.Batches = append(record.Batches, batch)
					return nil
				})
				if err != nil {
					return err
				}
				if mapped > math.MaxUint64-record.ZeroDateRows {
					return herror.NewError("CDC zero date row count overflow")
				}
				record.ZeroDateRows += mapped
			}
		}
		planner.reset()
		for {
			p.mu.Lock()
			changed := p.changed
			p.mu.Unlock()
			var err error
			if staged == nil {
				err = delivery.appendRecord(record, limit)
			} else {
				err = delivery.appendStagedRecord(staged)
			}
			if !errors.Is(err, errBufferFull) {
				if err != nil {
					return err
				}
				break
			}
			p.mu.Lock()
			if !p.status.Backpressured {
				p.status.Backpressured = true
				p.signalStatusLocked()
			}
			p.mu.Unlock()
			select {
			case <-ctx.Done():
				return herror.Wrap(ctx.Err())
			case <-changed:
			}
		}
		progress := delivery.Status()
		p.mu.Lock()
		p.updateProgressLocked(progress)
		p.status.Backpressured = false
		p.signalLocked()
		p.mu.Unlock()
		if version.Blocked {
			<-ctx.Done()
			return herror.Wrap(ctx.Err())
		}
		return nil
	}
	markReady := sync.OnceFunc(ready)
	const minBackoff = time.Second
	const maxBackoff = 30 * time.Second
	backoff := minBackoff
	subscribed := false
	for {
		cursor = mysqlsource.Cursor(delivery.Status().Durable.Cursor)
		err := source.StreamStaged(ctx, cursor, version.Tables, func() {
			markReady()
			if !subscribed {
				subscribed = true
				return
			}
			p.setCaptureError("")
		}, planner.stage, func(transaction mysqlsource.Transaction) error {
			if err := consume(transaction); err != nil {
				return err
			}
			backoff = minBackoff
			return nil
		})
		planner.reset()
		if !subscribed || !errors.Is(err, mysqlsource.ErrTransientStream) || ctx.Err() != nil {
			return err
		}
		if p.setCaptureError(err.Error()) {
			retryCursor := delivery.Status().Durable.Cursor
			hlog.Warn(ctx, "CDC binlog subscription will retry source_id=%s file=%s position=%d error=%v",
				config.Identity.SourceID, retryCursor.File, retryCursor.Position, err)
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return herror.Wrap(ctx.Err())
		case <-timer.C:
			backoff = min(backoff*2, maxBackoff)
		}
	}
}

// setCaptureError 更新采集重连状态，并报告重试原因是否变化。
func (p *Pipeline) setCaptureError(message string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status.CaptureError == message {
		return false
	}
	p.status.CaptureError = message
	p.signalLocked()
	return true
}

// deliver 顺序交付队首记录，处理暂停、审批、暂时失联及停止信号。
func (p *Pipeline) deliver(ctx context.Context, delivery *Delivery, history *spool.Store) error {
	var inspectedHead bool
	for {
		p.mu.Lock()
		if p.stopping {
			p.mu.Unlock()
			return nil
		}
		changed := p.changed
		parked := inspectedHead && p.deliveryBlockedLocked()
		p.mu.Unlock()
		if parked {
			select {
			case <-ctx.Done():
				return herror.Wrap(ctx.Err())
			case <-changed:
				continue
			}
		}

		var started bool
		var schemaBarrier bool
		progress, err := delivery.deliverNext(ctx, func(record *OrderedRecord) bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.stopping {
				return false
			}
			previousDDL, previousState, previousReason := p.status.PendingDDL, p.status.State, p.status.Reason
			p.status.PendingDDL = nil
			inspectedHead = record != nil
			if inspectedHead {
				p.status.PendingDDL = record.DDL
				schemaBarrier = record.DDL != nil
			}
			p.publishStateLocked()
			started = inspectedHead && !p.deliveryBlockedLocked()
			p.status.InFlight = started
			if started || p.status.PendingDDL != previousDDL || p.status.State != previousState || p.status.Reason != previousReason {
				p.signalStatusLocked()
			}
			return started
		})
		retry := errors.Is(err, ErrLoadPending) || errors.Is(err, errDorisTargetUnavailable)
		if started {
			cursorProgress := delivery.Status()
			p.mu.Lock()
			p.status.InFlight = false
			p.updateProgressLocked(cursorProgress)
			previousError := p.status.DeliveryError
			p.status.DeliveryError = ""
			if retry {
				p.status.DeliveryError = err.Error()
			}
			if progress {
				inspectedHead = false
				p.status.PendingDDL = nil
			}
			p.publishStateLocked()
			p.signalLocked()
			status := p.status
			p.mu.Unlock()
			if progress && schemaBarrier {
				if cleanupErr := reclaimSchemaHistory(history, cursorProgress.Delivered); cleanupErr != nil {
					hlog.Warn(ctx, "CDC schema history cleanup deferred source_id=%s error=%v", status.Identity.SourceID, cleanupErr)
				}
			}
			if retry && status.DeliveryError != previousError {
				hlog.Warn(ctx, "CDC delivery will retry source_id=%s sequence=%d error=%s", status.Identity.SourceID, status.Progress.Delivered.Sequence+1, status.DeliveryError)
			}
		}
		if retry {
			delay := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				delay.Stop()
				return herror.Wrap(ctx.Err())
			case <-delay.C:
			}
			continue
		}
		if err != nil {
			return err
		}
		if !started {
			select {
			case <-ctx.Done():
				return herror.Wrap(ctx.Err())
			case <-changed:
			}
		}
	}
}

// updateProgressLocked 丢弃被采集或交付并发发布的更新所超过的旧快照。
func (p *Pipeline) updateProgressLocked(progress DeliveryStatus) {
	if progress.Durable.Sequence >= p.status.Progress.Durable.Sequence &&
		progress.Delivered.Sequence >= p.status.Progress.Delivered.Sequence {
		p.status.Progress = progress
	}
}

// deliveryBlockedLocked 在持锁时判断当前控制状态或 DDL 屏障是否阻塞交付。
func (p *Pipeline) deliveryBlockedLocked() bool {
	plan := p.status.PendingDDL
	return p.controlWrite != nil || p.pendingControl != nil || p.pausedReason != "" ||
		(plan != nil && (plan.Kind == DDLUnsupported || (plan.RequiresApproval && !p.approved[plan.ID])))
}
