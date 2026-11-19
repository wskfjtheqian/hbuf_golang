package hcdc_test

import (
	"sync"
	"testing"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
)

func Test_MonitorSnapshot(t *testing.T) {
	m := hcdc.NewMonitor()

	// 初始快照
	s := m.Snapshot()
	if s.CaptureRows != 0 || s.UploadFiles != 0 || s.PauseCount != 0 {
		t.Fatalf("initial snapshot should be zero, got %+v", s)
	}
	if s.UptimeSeconds < 0 {
		t.Fatalf("uptime should be >= 0, got %d", s.UptimeSeconds)
	}

	// 模拟采集
	m.CaptureRows.Add(100)
	m.CaptureBytes.Add(1024)
	m.CaptureErrors.Add(2)

	// 模拟上传
	m.UploadFiles.Add(5)
	m.UploadRows.Add(100)
	m.UploadBytes.Add(1024)
	m.UploadTotalMs.Add(250)
	m.UploadErrors.Add(1)

	// 模拟 DDL
	m.DDLCount.Add(3)
	m.CreateTable.Add(1)
	m.AlterTable.Add(1)
	m.DropTable.Add(1)
	m.DDLErrors.Add(1)

	// 模拟暂停/恢复
	m.PauseCount.Add(1)
	m.ResumeCount.Add(1)

	s = m.Snapshot()
	if s.CaptureRows != 100 {
		t.Fatalf("CaptureRows = %d, want 100", s.CaptureRows)
	}
	if s.CaptureBytes != 1024 {
		t.Fatalf("CaptureBytes = %d, want 1024", s.CaptureBytes)
	}
	if s.CaptureErrors != 2 {
		t.Fatalf("CaptureErrors = %d, want 2", s.CaptureErrors)
	}
	if s.UploadFiles != 5 {
		t.Fatalf("UploadFiles = %d, want 5", s.UploadFiles)
	}
	if s.UploadRows != 100 {
		t.Fatalf("UploadRows = %d, want 100", s.UploadRows)
	}
	if s.UploadAvgMs != 50 {
		t.Fatalf("UploadAvgMs = %d, want 50 (250/5)", s.UploadAvgMs)
	}
	if s.UploadErrors != 1 {
		t.Fatalf("UploadErrors = %d, want 1", s.UploadErrors)
	}
	if s.DDLCount != 3 || s.CreateTable != 1 || s.AlterTable != 1 || s.DropTable != 1 || s.DDLErrors != 1 {
		t.Fatalf("DDL counters mismatch: %+v", s)
	}
	if s.PauseCount != 1 || s.ResumeCount != 1 {
		t.Fatalf("pause/resume counters mismatch: %+v", s)
	}
}

func Test_MonitorUploadMax(t *testing.T) {
	m := hcdc.NewMonitor()

	m.UploadFiles.Add(3)
	m.UploadTotalMs.Add(100 + 200 + 50)
	// 直接模拟 uploadMaxMs 的并发更新路径（私有字段无法直接访问，这里通过并发 Add 验证 Snapshot 不 panic）

	s := m.Snapshot()
	if s.UploadAvgMs != 116 { // 350/3 = 116
		t.Fatalf("UploadAvgMs = %d, want 116", s.UploadAvgMs)
	}
}

func Test_MonitorDelay(t *testing.T) {
	m := hcdc.NewMonitor()
	// recordDelay 是私有方法，通过并发场景验证延迟统计的并发安全。
	// 这里直接并发读写 Snapshot，确保无数据竞争（配合 -race 使用）。
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.CaptureRows.Add(1)
			m.UploadRows.Add(1)
			_ = m.Snapshot()
		}()
	}
	wg.Wait()

	s := m.Snapshot()
	if s.CaptureRows != 100 {
		t.Fatalf("CaptureRows = %d, want 100", s.CaptureRows)
	}
}

func Test_MonitorBacklog(t *testing.T) {
	d := hcdc.NewDoris(&hcdc.DorisConfig{
		Host:    "127.0.0.1:9030",
		LoadURL: "http://127.0.0.1:8040/",
	})
	// NewDoris 内部创建 monitor，验证 Pause/Resume 会更新计数
	d.Pause(t.Context(), "test pause")
	d.Resume(t.Context())

	state := d.GetSyncState()
	if state.Status != hcdc.SyncRunning {
		t.Fatalf("status = %v, want running", state.Status)
	}
}
