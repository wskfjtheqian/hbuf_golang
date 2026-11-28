// Package monitor 提供可嵌入的 CDC 状态快照、监听和逐源控制。
package monitor

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// Monitor 管理可重载的源目录。调用方可以独立启动、观察和控制每个源。
type Monitor struct {
	ctx     context.Context
	manager *hcdc.Manager
	updates chan struct{}

	mu            sync.RWMutex
	order         []*source
	byID          map[string]*source
	stopDiscovery context.CancelFunc
	reloading     bool
	closed        bool
}

type source struct {
	resolved hcdc.ResolvedSource
	pipeline atomic.Pointer[hcdc.Pipeline]
	control  chan struct{}
}

// New 创建源目录为空的监控。通过 Reload 加载配置，ctx 决定流水线生命周期。
func New(ctx context.Context) *Monitor {
	return &Monitor{
		ctx: ctx, manager: hcdc.NewManager(ctx), updates: make(chan struct{}, 1),
	}
}

// monitorSources 为已发现的源创建监控目录。
func monitorSources(sources []hcdc.ResolvedSource) ([]*source, map[string]*source) {
	order := make([]*source, 0, len(sources))
	byID := make(map[string]*source, len(sources))
	for _, resolved := range sources {
		s := &source{resolved: resolved, control: make(chan struct{}, 1)}
		order = append(order, s)
		byID[resolved.Identity().SourceID] = s
	}
	return order, byID
}

// Snapshot 返回配置顺序下的全部源状态。各源独立采样，快照不读取数据库。
func (m *Monitor) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := Snapshot{Sources: make([]SourceStatus, 0, len(m.order))}
	for _, s := range m.order {
		result.Sources = append(result.Sources, s.status(m.currentPipelineLocked(s), m.closedLocked()))
	}
	result.UpdatedAt = time.Now().UTC()
	return result
}

// Status 返回指定源的状态，包括未启动和已经停止的源。
func (m *Monitor) Status(sourceID string) (SourceStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.byID[sourceID]
	if s == nil {
		return SourceStatus{}, sourceError(sourceID, ErrSourceNotFound)
	}
	return s.status(m.currentPipelineLocked(s), m.closedLocked()), nil
}

// WatchStatus 立即发送指定源的快照，再串行回调当前流水线的状态。
// 未启动的源只发送一次。流水线更换后，调用方可用同一源 ID 重新监听。
// 回调在锁外执行，状态变化过快时合并为最新快照。取消 ctx 只结束监听。
func (m *Monitor) WatchStatus(ctx context.Context, sourceID string, callback func(context.Context, SourceStatus)) error {
	if callback == nil {
		return herror.NewError("CDC monitor status callback is required")
	}
	if err := ctx.Err(); err != nil {
		return herror.Wrap(err)
	}
	m.mu.RLock()
	s := m.byID[sourceID]
	if s == nil {
		m.mu.RUnlock()
		return sourceError(sourceID, ErrSourceNotFound)
	}
	p, closed := m.currentPipelineLocked(s), m.closedLocked()
	m.mu.RUnlock()
	if p == nil {
		if err := ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
		callback(ctx, s.status(nil, closed))
		return nil
	}
	return p.WatchStatus(ctx, func(ctx context.Context, state hcdc.PipelineStatus) {
		// 终态发布紧接着关闭 Done，确保终态回调可以再次启动该源。
		if state.State == hcdc.PipelineFailed || state.State == hcdc.PipelineStopped {
			<-p.Done()
		}
		m.mu.RLock()
		current := m.byID[sourceID]
		available := current != nil && !m.closedLocked() && m.currentPipelineLocked(current) == p
		m.mu.RUnlock()
		callback(ctx, s.snapshot(state, pipelineDone(p), !available))
	})
}

// Close 阻止新控制操作，并等待管理器关闭受管流水线。超时后可以再次调用等待释放。
func (m *Monitor) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	if m.stopDiscovery != nil {
		m.stopDiscovery()
		m.stopDiscovery = nil
	}
	m.mu.Unlock()
	if err := acquire(ctx, m.updates); err != nil {
		return err
	}
	defer func() { <-m.updates }()
	return m.manager.SetConfig(ctx, nil)
}

// closedLocked 在持有目录锁时判断监控是否还接受操作。
func (m *Monitor) closedLocked() bool {
	return m.closed || m.ctx.Err() != nil
}

// currentPipelineLocked 取得最新恢复句柄。重载提交目录前，读取仍使用原配置对应的句柄。
func (m *Monitor) currentPipelineLocked(s *source) *hcdc.Pipeline {
	if m.reloading {
		return s.pipeline.Load()
	}
	return m.refreshPipeline(s)
}

// refreshPipeline 保存管理器安装的句柄，移除源后仍能读取最后一份状态。
func (m *Monitor) refreshPipeline(s *source) *hcdc.Pipeline {
	identity := s.resolved.Identity()
	if p, ok := m.manager.Pipeline(identity.SourceID); ok && p.Status().Identity == identity {
		s.pipeline.Store(p)
		return p
	}
	return s.pipeline.Load()
}

// changeSource 应用手动启动或停止后的运行源清单。调用方持有 updates。
func (m *Monitor) changeSource(ctx context.Context, chosen *source, start bool) error {
	var desired []hcdc.ResolvedSource
	m.mu.RLock()
	for _, s := range m.order {
		if s == chosen {
			if start {
				desired = append(desired, s.resolved)
			}
			continue
		}
		if p := m.currentPipelineLocked(s); p != nil && !pipelineDone(p) {
			desired = append(desired, s.resolved)
		}
	}
	m.mu.RUnlock()
	err := m.manager.ApplySources(ctx, desired)
	m.refreshPipeline(chosen)
	return err
}

// Reload 发现 Canal 选中的源库和表，并更新源目录。
// 已经运行的源通过管理器交接配置。
// 显式加载发现的新增源保持空闲。随后后台自动发现并启动新库。
// 应用失败时保留原目录，管理器负责同代次配置恢复。
func (m *Monitor) Reload(ctx context.Context, config *hcdc.Config) error {
	if err := acquire(ctx, m.updates); err != nil {
		return err
	}
	defer func() { <-m.updates }()
	m.mu.RLock()
	closed := m.closedLocked()
	m.mu.RUnlock()
	if closed {
		return ErrClosed
	}
	config = copyDiscoveryConfig(config)
	resolved, err := config.DiscoverSchemas(ctx, nil)
	if err != nil {
		return err
	}
	order, byID := monitorSources(resolved)
	var desired []hcdc.ResolvedSource
	m.mu.Lock()
	if m.closedLocked() {
		m.mu.Unlock()
		return ErrClosed
	}
	for _, s := range order {
		identity := s.resolved.Identity()
		old := m.byID[identity.SourceID]
		if old == nil {
			continue
		}
		if p := m.currentPipelineLocked(old); p != nil {
			if old.resolved.Identity() == identity {
				s.pipeline.Store(p)
			}
			if !pipelineDone(p) {
				desired = append(desired, s.resolved)
			}
		}
	}
	m.reloading = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.reloading = false
		m.mu.Unlock()
	}()
	if err := m.manager.ApplySources(ctx, desired); err != nil {
		return err
	}
	for _, s := range order {
		m.refreshPipeline(s)
	}
	m.mu.Lock()
	m.order, m.byID = order, byID
	m.restartDiscoveryLocked(config)
	m.mu.Unlock()
	return nil
}

// acquire 在调用期限内取得指定的操作权。
func acquire(ctx context.Context, gate chan struct{}) error {
	select {
	case gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate
			return herror.Wrap(err)
		}
		return nil
	case <-ctx.Done():
		return herror.Wrap(ctx.Err())
	}
}

// pipelineDone 检查流水线是否已释放资源。
func pipelineDone(p *hcdc.Pipeline) bool {
	select {
	case <-p.Done():
		return true
	default:
		return false
	}
}
