package monitor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
)

// TestMonitorSDKLifecycle 验证状态快照、按源控制、目录更新和服务关闭。
func TestMonitorSDKLifecycle(t *testing.T) {
	ctx := t.Context()
	primary := monitorTestPipeline(t, "primary/source", "orders_shadow", 72001)
	second := monitorTestPipeline(t, "second", "payments_shadow", 72002)
	config := &hcdc.Config{Pipelines: []hcdc.PipelineConfig{primary, second}}
	m, err := New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close(context.Background())) })
	require.Empty(t, m.Snapshot().Sources)
	require.ErrorIs(t, m.Start(ctx, primary.Identity.SourceID), ErrSourceNotFound)
	require.NoError(t, m.Reload(ctx, config))
	config.Pipelines[0].Source.Tables[0] = "changed"
	require.Equal(t, "events", m.byID[primary.Identity.SourceID].config.Source.Tables[0])

	initial := m.Snapshot()
	require.NotZero(t, initial.UpdatedAt)
	require.Len(t, initial.Sources, 2)
	require.Equal(t, primary.Identity.SourceID, initial.Sources[0].ID)
	require.Equal(t, second.Identity.SourceID, initial.Sources[1].ID)
	require.Equal(t, StateNotStarted, initial.Sources[0].State)
	require.Equal(t, []Action{ActionStart}, initial.Sources[0].Actions)
	const largeCounter = 1<<54 + 17
	initial.Sources[0].Delivered.Sequence = largeCounter
	initial.Sources[0].SnapshotZeroDateRows = largeCounter
	encoded, err := json.Marshal(initial)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"sequence":"18014398509482001"`)
	require.Contains(t, string(encoded), `"snapshotZeroDateRows":"18014398509482001"`)
	require.NotContains(t, string(encoded), "source-secret")
	require.NotContains(t, string(encoded), "doris-secret")

	initial.Sources[0].Actions[0] = ActionStop
	status, err := m.Status(primary.Identity.SourceID)
	require.NoError(t, err)
	require.Zero(t, status.Delivered.Sequence)
	require.Equal(t, []Action{ActionStart}, status.Actions)
	_, err = m.Status("unknown")
	require.ErrorIs(t, err, ErrSourceNotFound)
	require.ErrorIs(t, m.Start(ctx, "unknown"), ErrSourceNotFound)
	require.ErrorIs(t, m.Pause(ctx, primary.Identity.SourceID, "维护"), ErrInvalidState)
	require.ErrorIs(t, m.Resume(ctx, primary.Identity.SourceID), ErrInvalidState)
	require.ErrorIs(t, m.Stop(ctx, primary.Identity.SourceID), ErrInvalidState)
	require.ErrorIs(t, m.ApproveDDL(ctx, primary.Identity.SourceID, "ddl"), ErrInvalidState)

	blockMonitorTestBootstrap(t, primary)
	require.NoError(t, m.Start(ctx, primary.Identity.SourceID))
	waitForMonitorFailure(t, m, primary.Identity.SourceID)
	status, err = m.Status(second.Identity.SourceID)
	require.NoError(t, err)
	require.Equal(t, StateNotStarted, status.State)

	replacement := monitorTestPipeline(t, "replacement", "replacement_shadow", 72003)
	require.NoError(t, m.Reload(ctx, &hcdc.Config{Pipelines: []hcdc.PipelineConfig{replacement}}))
	require.Error(t, m.Reload(ctx, nil))
	_, err = m.Status(primary.Identity.SourceID)
	require.ErrorIs(t, err, ErrSourceNotFound)
	status, err = m.Status(replacement.Identity.SourceID)
	require.NoError(t, err)
	require.Equal(t, StateNotStarted, status.State)

	require.NoError(t, m.Close(ctx))
	require.ErrorIs(t, m.Start(ctx, replacement.Identity.SourceID), ErrClosed)
	require.ErrorIs(t, m.Reload(ctx, &hcdc.Config{Pipelines: []hcdc.PipelineConfig{replacement}}), ErrClosed)
}

// TestMonitorWatchStatus 验证空闲源只通知一次，订阅退出时保持原流水线句柄。
func TestMonitorWatchStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	first := monitorTestPipeline(t, "source", "orders_shadow", 72001)
	m, err := New(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close(context.Background())) })
	require.NoError(t, m.Reload(ctx, &hcdc.Config{Pipelines: []hcdc.PipelineConfig{first}}))

	var idle []SourceStatus
	require.NoError(t, m.WatchStatus(ctx, first.Identity.SourceID, func(_ context.Context, state SourceStatus) {
		idle = append(idle, state)
		_, err := m.Status(state.ID)
		require.NoError(t, err)
	}))
	require.Len(t, idle, 1)
	require.Equal(t, StateNotStarted, idle[0].State)
	require.ErrorIs(t, m.WatchStatus(ctx, "unknown", func(context.Context, SourceStatus) {}), ErrSourceNotFound)
	canceled, stop := context.WithCancel(ctx)
	stop()
	require.ErrorIs(t, m.WatchStatus(canceled, first.Identity.SourceID, func(context.Context, SourceStatus) {
		t.Fatal("取消的监听收到了通知")
	}), context.Canceled)

	blockMonitorTestBootstrap(t, first)
	require.NoError(t, m.Start(ctx, first.Identity.SourceID))
	states := make(chan SourceStatus, 8)
	finished := make(chan error, 1)
	release := make(chan struct{})
	finishCallback := sync.OnceFunc(func() { close(release) })
	defer finishCallback()
	go func() {
		finished <- m.WatchStatus(ctx, first.Identity.SourceID, func(callbackCtx context.Context, status SourceStatus) {
			states <- status
			if status.State == hcdc.PipelineFailed {
				select {
				case <-release:
				case <-callbackCtx.Done():
				}
			}
		})
	}()
	for {
		select {
		case status := <-states:
			require.Equal(t, first.Identity.Generation, status.Generation)
			if status.State != hcdc.PipelineFailed {
				continue
			}
			require.NotEmpty(t, status.Error)
		case <-ctx.Done():
			t.Fatal("监听未报告流水线失败")
		}
		break
	}
	waitForMonitorFailure(t, m, first.Identity.SourceID)

	second := monitorTestPipeline(t, first.Identity.SourceID, "orders_shadow_next", 72001)
	second.Identity.Generation = "g02"
	blockMonitorTestBootstrap(t, second)
	require.NoError(t, m.Reload(ctx, &hcdc.Config{Pipelines: []hcdc.PipelineConfig{second}}))
	require.NoError(t, m.Start(ctx, second.Identity.SourceID))
	finishCallback()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("监听在报告原流水线终态后没有退出")
	}
	require.Empty(t, states, "监听不能跟随被替换的流水线")
	require.Equal(t, second.Identity.Generation, waitForMonitorFailure(t, m, second.Identity.SourceID).Generation)
}

// monitorTestPipeline 返回无需连接真实数据库即可校验的源配置。
func monitorTestPipeline(t *testing.T, id, target string, serverID uint32) hcdc.PipelineConfig {
	t.Helper()
	return hcdc.PipelineConfig{
		Identity:  hcdc.StreamIdentity{PipelineID: "test", SourceID: id, Generation: "g01"},
		Directory: t.TempDir(),
		Source: hcdc.MySQLConfig{
			Address: "localhost:3306", User: "cdc", Password: "source-secret", SourceIdentity: id,
			ServerID: serverID, Database: "orders", Tables: []string{"events"},
		},
		Doris: hcdc.DorisConfig{
			SQLDSN:      "cdc:doris-secret@tcp(127.0.0.1:9030)/",
			FrontendURL: "http://localhost:8030", StreamLoadURL: "http://localhost:8040",
		},
		TargetDatabase: target,
	}
}

// blockMonitorTestBootstrap 使流水线在打开源连接前异步失败。
func blockMonitorTestBootstrap(t *testing.T, config hcdc.PipelineConfig) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(config.Directory, "bootstrap"), nil, 0o600))
}

// waitForMonitorFailure 等待终态并确认流水线已经允许重新启动。
func waitForMonitorFailure(t *testing.T, m *Monitor, sourceID string) SourceStatus {
	t.Helper()
	var status SourceStatus
	require.Eventually(t, func() bool {
		var err error
		status, err = m.Status(sourceID)
		return err == nil && status.State == hcdc.PipelineFailed && len(status.Actions) == 1 && status.Actions[0] == ActionStart
	}, 2*time.Second, time.Millisecond)
	require.NotEmpty(t, status.Error)
	return status
}
