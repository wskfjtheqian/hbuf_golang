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
	output := SourceStatus{
		ID: s.config.Identity.SourceID, Generation: s.config.Identity.Generation, State: state.State,
		Reason: s.redactor.Replace(state.Reason), DeliveryError: s.redactor.Replace(state.DeliveryError),
		CaptureError: s.redactor.Replace(state.CaptureError),
		PendingBytes: state.Progress.PendingBytes, Backpressured: state.Backpressured, InFlight: state.InFlight,
		ReadCursor: snapshotCursor(state.ReadCursor), Durable: snapshotCheckpoint(state.Progress.Durable),
		Delivered: snapshotCheckpoint(state.Progress.Delivered), SnapshotZeroDateRows: state.SnapshotZeroDateRows,
		Actions: availableActions(state.State, done, closed),
	}
	if !state.Since.IsZero() {
		since := state.Since.UTC()
		output.Since = &since
	}
	if state.Err != nil {
		output.Error = s.redactor.Replace(state.Err.Error())
	}
	if plan := state.PendingDDL; plan != nil {
		output.PendingDDL = &DDLStatus{
			ID: plan.ID, Kind: plan.Kind, Table: plan.Before.Database + "." + plan.Before.Name,
			Reason: s.redactor.Replace(plan.Reason), RequiresApproval: plan.RequiresApproval,
		}
	}
	return output
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
