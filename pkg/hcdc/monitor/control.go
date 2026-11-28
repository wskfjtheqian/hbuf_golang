package monitor

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// ErrClosed 表示监控已停止接受控制操作。
var ErrClosed = herror.NewError("CDC monitor is closed")

// ErrSourceNotFound 表示源 ID 不在当前配置中。
var ErrSourceNotFound = herror.NewError("CDC monitor source was not found")

// ErrInvalidState 表示当前流水线状态不接受所请求的操作。
var ErrInvalidState = herror.NewError("CDC monitor source does not accept this action in its current state")

// Start 异步启动指定源。启动结果可从 Status 或 WatchStatus 获取。
func (m *Monitor) Start(ctx context.Context, sourceID string) error {
	return m.control(ctx, sourceID, ActionStart, "")
}

// Pause 暂停指定源向 Doris 交付，采集继续到缓冲上限。
func (m *Monitor) Pause(ctx context.Context, sourceID, reason string) error {
	return m.control(ctx, sourceID, ActionPause, strings.TrimSpace(reason))
}

// Resume 清除指定源的手动暂停状态。DDL 审批通过 ApproveDDL 处理。
func (m *Monitor) Resume(ctx context.Context, sourceID string) error {
	return m.control(ctx, sourceID, ActionResume, "")
}

// Stop 停止指定源，并在 ctx 的期限内等待流水线释放资源。
func (m *Monitor) Stop(ctx context.Context, sourceID string) error {
	return m.control(ctx, sourceID, ActionStop, "")
}

// ApproveDDL 审批指定源当前等待确认且允许执行的 DDL。
func (m *Monitor) ApproveDDL(ctx context.Context, sourceID, ddlID string) error {
	return m.control(ctx, sourceID, actionApproveDDL, ddlID)
}

const actionApproveDDL Action = "approve_ddl"

// sourceError 为 SDK 错误附上源标识，并保留 errors.Is 的分类能力。
func sourceError(sourceID string, err error) error {
	return herror.WrapMsg(err, fmt.Sprintf("CDC source %q", sourceID))
}

// control 取得控制权后复查实际句柄和状态，再调用流水线或管理器。
func (m *Monitor) control(ctx context.Context, sourceID string, action Action, value string) error {
	if err := ctx.Err(); err != nil {
		return herror.Wrap(err)
	}
	if action == ActionStart || action == ActionStop {
		if err := acquire(ctx, m.updates); err != nil {
			return err
		}
		defer func() { <-m.updates }()
	}
	m.mu.RLock()
	s, closed := m.byID[sourceID], m.closedLocked()
	m.mu.RUnlock()
	if closed {
		return ErrClosed
	}
	if s == nil {
		return sourceError(sourceID, ErrSourceNotFound)
	}
	if err := acquire(ctx, s.control); err != nil {
		return err
	}
	defer func() { <-s.control }()

	m.mu.RLock()
	current, closed := m.byID[sourceID], m.closedLocked()
	if closed || current != s {
		m.mu.RUnlock()
		if closed {
			return ErrClosed
		}
		return sourceError(sourceID, ErrInvalidState)
	}
	p := m.currentPipelineLocked(s)
	m.mu.RUnlock()

	state, done := pipelineStatus(p)
	if action == actionApproveDDL {
		plan := state.PendingDDL
		if done || plan == nil || !plan.RequiresApproval || plan.Kind == hcdc.DDLUnsupported || plan.ID != value {
			return sourceError(sourceID, ErrInvalidState)
		}
		return p.ApproveDDL(ctx, value)
	}
	if !slices.Contains(availableActions(state.State, done, false), action) {
		return sourceError(sourceID, ErrInvalidState)
	}
	switch action {
	case ActionStart:
		return m.changeSource(ctx, s, true)
	case ActionPause:
		return p.Pause(ctx, value)
	case ActionResume:
		return p.Resume(ctx)
	case ActionStop:
		return m.changeSource(ctx, s, false)
	default:
		return sourceError(sourceID, ErrInvalidState)
	}
}
