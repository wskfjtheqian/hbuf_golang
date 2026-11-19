package hcdc_test

import (
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
)

func Test_DorisSyncState(t *testing.T) {
	d := hcdc.NewDoris(&hcdc.DorisConfig{
		Host:    "127.0.0.1:9030",
		LoadURL: "http://127.0.0.1:8040/",
	})

	// 初始状态应为 running
	state := d.GetSyncState()
	if state.Status != hcdc.SyncRunning {
		t.Fatalf("initial status = %v, want running", state.Status)
	}
	if d.IsPaused() {
		t.Fatalf("initial IsPaused = true, want false")
	}

	// 手动暂停
	d.Pause(t.Context(), "manual pause for maintenance")
	state = d.GetSyncState()
	if state.Status != hcdc.SyncPaused {
		t.Fatalf("status after Pause = %v, want paused", state.Status)
	}
	if !d.IsPaused() {
		t.Fatalf("IsPaused after Pause = false, want true")
	}
	if state.Reason != "manual pause for maintenance" {
		t.Fatalf("reason = %q, want manual pause for maintenance", state.Reason)
	}
	if state.PausedAt.IsZero() {
		t.Fatalf("PausedAt is zero, want set")
	}

	// 再次暂停不应刷新 PausedAt
	firstPausedAt := state.PausedAt
	time.Sleep(10 * time.Millisecond)
	d.Pause(t.Context(), "second pause")
	state = d.GetSyncState()
	if !state.PausedAt.Equal(firstPausedAt) {
		t.Fatalf("PausedAt changed on repeated pause: %v -> %v", firstPausedAt, state.PausedAt)
	}

	// 手动恢复
	d.Resume(t.Context())
	state = d.GetSyncState()
	if state.Status != hcdc.SyncRunning {
		t.Fatalf("status after Resume = %v, want running", state.Status)
	}
	if d.IsPaused() {
		t.Fatalf("IsPaused after Resume = true, want false")
	}
	if !state.PausedAt.IsZero() {
		t.Fatalf("PausedAt after Resume = %v, want zero", state.PausedAt)
	}
}

func Test_DorisSyncStateString(t *testing.T) {
	if hcdc.SyncRunning.String() != "running" {
		t.Fatalf("SyncRunning.String() = %q, want running", hcdc.SyncRunning.String())
	}
	if hcdc.SyncPaused.String() != "paused" {
		t.Fatalf("SyncPaused.String() = %q, want paused", hcdc.SyncPaused.String())
	}
}
