package monitor

import (
	"context"
	"slices"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// copyDiscoveryConfig 保留独立的配置副本供后台发现使用。
func copyDiscoveryConfig(config *hcdc.Config) *hcdc.Config {
	if config == nil {
		return nil
	}
	copied := *config
	if config.Doris != nil {
		doris := *config.Doris
		copied.Doris = &doris
	}
	copied.Canals = slices.Clone(config.Canals)
	for i := range copied.Canals {
		canal := &copied.Canals[i]
		canal.IncludeDBs = slices.Clone(canal.IncludeDBs)
		canal.ExcludeDBs = slices.Clone(canal.ExcludeDBs)
		canal.IncludeTables = slices.Clone(canal.IncludeTables)
		canal.ExcludeTables = slices.Clone(canal.ExcludeTables)
		if canal.ServerID != nil {
			serverID := *canal.ServerID
			canal.ServerID = &serverID
		}
	}
	return &copied
}

// restartDiscoveryLocked 在目录提交后安排新一轮发现，并取消旧配置的发现任务。
func (m *Monitor) restartDiscoveryLocked(config *hcdc.Config) {
	if m.stopDiscovery != nil {
		m.stopDiscovery()
		m.stopDiscovery = nil
	}
	if m.closedLocked() || len(config.Canals) == 0 {
		return
	}
	interval := config.AutoDiscoverInterval
	if interval == 0 {
		interval = hcdc.DefaultAutoDiscoverInterval
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.stopDiscovery = cancel
	go m.discoverSchemas(ctx, config, interval)
}

// discoverSchemas 定期读取源端元数据，并追加新发现的库。
func (m *Monitor) discoverSchemas(ctx context.Context, config *hcdc.Config, interval time.Duration) {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	var previousError string
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		m.mu.RLock()
		known := make([]hcdc.ResolvedSource, 0, len(m.order))
		for _, s := range m.order {
			known = append(known, s.resolved)
		}
		m.mu.RUnlock()
		candidates, err := config.DiscoverSchemas(ctx, known)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			previousError = ""
		} else if message := err.Error(); message != previousError {
			hlog.Warn(ctx, "CDC 自动发现源库: %s", message)
			previousError = message
		}
		if len(candidates) != 0 {
			m.startDiscoveredSources(ctx, candidates)
		}
		timer.Reset(interval)
	}
}

// startDiscoveredSources 在控制操作之间追加新源，已有源保持原句柄。
func (m *Monitor) startDiscoveredSources(ctx context.Context, candidates []hcdc.ResolvedSource) {
	if acquire(ctx, m.updates) != nil {
		return
	}
	defer func() { <-m.updates }()
	for _, candidate := range candidates {
		id := candidate.Identity().SourceID
		m.mu.RLock()
		closed, known := m.closedLocked(), m.byID[id] != nil
		m.mu.RUnlock()
		if closed || ctx.Err() != nil {
			return
		}
		if known {
			continue
		}
		pipeline, err := m.manager.StartDiscoveredSource(ctx, candidate)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			hlog.Warn(ctx, "CDC 自动启动源库失败 source_id=%s error=%s", id, candidate.RedactMessage(err.Error()))
			continue
		}
		s := &source{resolved: candidate, control: make(chan struct{}, 1)}
		s.pipeline.Store(pipeline)
		m.mu.Lock()
		m.order = append(m.order, s)
		m.byID[id] = s
		m.mu.Unlock()
		hlog.Info(ctx, "CDC 已自动启动新源库 source_id=%s target_database=%s", id, candidate.TargetDatabase())
	}
}
