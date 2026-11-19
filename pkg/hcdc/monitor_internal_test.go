package hcdc

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Test_ParseCaptureTime 验证 .active 文件名中采集时间戳的解析。
func Test_ParseCaptureTime(t *testing.T) {
	nano := int64(1700000000123456789)
	want := time.Unix(0, nano)

	// 标准 4 段格式：{binlogName}_{pos}_{gtidB64}_{captureNano}.csv.active
	// gtidB64 为空字符串（未开启 GTID）时对应文件名里是连续两个下划线。
	path := filepath.Join("tmp", "schema", "table", "mysql-bin.000001_1234__"+strconv.FormatInt(nano, 10)+".csv.active")
	got := parseCaptureTime(path)
	if !got.Equal(want) {
		t.Fatalf("parseCaptureTime = %v, want %v", got, want)
	}

	// gtid 段非空（base64）时仍能正确解析出时间戳。
	pathGTID := filepath.Join("tmp", "schema", "table", "mysql-bin.000001_1234_MDUyNzE3_"+strconv.FormatInt(nano, 10)+".csv.active")
	if !parseCaptureTime(pathGTID).Equal(want) {
		t.Fatalf("含 GTID 段的文件名应正确解析时间戳")
	}

	// 位点文件（SELECT_ 前缀）无采集时间语义，返回零值。
	if !parseCaptureTime(filepath.Join("tmp", "SELECT_xxx.csv.active")).IsZero() {
		t.Fatal("SELECT_ 前缀文件应返回零值时间")
	}

	// 格式不匹配时返回零值，不 panic。
	if !parseCaptureTime(filepath.Join("tmp", "no_underscore.csv.active")).IsZero() {
		t.Fatal("格式不匹配应返回零值时间")
	}
	if !parseCaptureTime(filepath.Join("tmp", "a_b.csv.active")).IsZero() {
		t.Fatal("两段格式应返回零值时间")
	}
}

// Test_Monitor_recordDelay 验证延迟统计的平均值与最大值。
func Test_Monitor_recordDelay(t *testing.T) {
	m := NewMonitor()

	m.recordDelay(100)
	m.recordDelay(300)
	m.recordDelay(-50) // 负延迟（时钟回拨）应被忽略

	s := m.Snapshot()
	if s.DelaySamples != 2 {
		t.Fatalf("DelaySamples = %d, want 2", s.DelaySamples)
	}
	if s.DelayAvgMs != 200 {
		t.Fatalf("DelayAvgMs = %d, want 200 (avg of 100,300)", s.DelayAvgMs)
	}
	if s.DelayMaxMs != 300 {
		t.Fatalf("DelayMaxMs = %d, want 300", s.DelayMaxMs)
	}
}

// Test_Monitor_setBacklog 验证积压统计。
func Test_Monitor_setBacklog(t *testing.T) {
	m := NewMonitor()

	m.setBacklog(3, 2048)
	s := m.Snapshot()
	if s.PendingFiles != 3 || s.PendingBytes != 2048 {
		t.Fatalf("backlog = %d/%d, want 3/2048", s.PendingFiles, s.PendingBytes)
	}

	m.setBacklog(0, 0)
	s = m.Snapshot()
	if s.PendingFiles != 0 || s.PendingBytes != 0 {
		t.Fatalf("backlog should reset to 0, got %d/%d", s.PendingFiles, s.PendingBytes)
	}
}

// Test_File_Backlog 验证 File 层积压统计（真实文件系统）。
func Test_File_Backlog(t *testing.T) {
	dir := t.TempDir()
	f := NewFile(0, "db", "tbl", dir)

	// 创建一个 .active 文件
	activeDir := filepath.Join(dir, "db", "tbl")
	if err := os.MkdirAll(activeDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("header\nrow1\nrow2\n")
	p1 := filepath.Join(activeDir, "bin.000001_100_1700000000000000000.csv.active")
	if err := os.WriteFile(p1, content, 0644); err != nil {
		t.Fatal(err)
	}
	p2 := filepath.Join(activeDir, "bin.000001_200_1700000000000000001.csv.active")
	if err := os.WriteFile(p2, []byte("header\nrow3\n"), 0644); err != nil {
		t.Fatal(err)
	}

	files, bytes := f.Backlog()
	if files != 2 {
		t.Fatalf("files = %d, want 2", files)
	}
	if bytes != int64(len(content)+len("header\nrow3\n")) {
		t.Fatalf("bytes = %d, want %d", bytes, len(content)+len("header\nrow3\n"))
	}
}
