package hlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogName(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 5, 0, time.Local)
	name, link := logName("INFO", now)
	want := fmt.Sprintf("%s.%s.%s.INFO.20261018-120005.%d.log", program, host, userName, pid)
	if name != want {
		t.Fatalf("logName = %q, want %q", name, want)
	}
	if link != program+".INFO" {
		t.Fatalf("link = %q, want %q", link, program+".INFO")
	}
}

func TestLogNameSameSecondSameName(t *testing.T) {
	// logName 由秒级时间戳 + pid 决定: 同一秒内多次调用返回相同文件名。
	// create 已通过 O_EXCL + 序号后缀处理同名冲突, 不会覆盖旧文件。
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	n1, _ := logName("ERROR", now)
	n2, _ := logName("ERROR", now.Add(500*time.Millisecond))
	if n1 != n2 {
		t.Fatalf("same second must produce same name: %q vs %q", n1, n2)
	}
}

func TestNewFileWriterWriteAndFlush(t *testing.T) {
	dir := t.TempDir()
	w := newFileWriter(dir, INFO)

	msg := "hello hlog\n"
	n, err := w.Write(INFO, []byte(msg))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(msg) {
		t.Fatalf("Write n = %d, want %d", n, len(msg))
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	_ = w.file.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != msg {
		t.Fatalf("file content = %q, want %q", data, msg)
	}
}

func TestFileWriterComplexCompare(t *testing.T) {
	dir := t.TempDir()
	w := newFileWriter(dir, ERROR)
	w.Compare(func(v1 Level, v2 Level) bool { return v1 <= v2 }) // 复杂模式

	// 低级别日志也写入高级别文件
	n, err := w.Write(INFO, []byte("info\n"))
	if err != nil {
		t.Fatalf("Write INFO: %v", err)
	}
	if n == 0 {
		t.Fatal("INFO must be written by ERROR writer in complex mode")
	}
	// 本级别日志写入
	n, err = w.Write(ERROR, []byte("error\n"))
	if err != nil {
		t.Fatalf("Write ERROR: %v", err)
	}
	if n == 0 {
		t.Fatal("ERROR must be written by ERROR writer")
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	_ = w.file.Close()
}

func TestFileWriterSimpleCompare(t *testing.T) {
	// 简单模式: 仅本级别日志写入(与 -log_simple 描述一致)
	dir := t.TempDir()
	w := newFileWriter(dir, ERROR)
	w.Compare(func(v1 Level, v2 Level) bool { return v1 == v2 }) // 简单模式

	n, err := w.Write(ERROR, []byte("error\n"))
	if err != nil {
		t.Fatalf("Write ERROR: %v", err)
	}
	if n == 0 {
		t.Fatal("ERROR must be written under simple mode")
	}
	n, err = w.Write(INFO, []byte("info\n"))
	if err != nil {
		t.Fatalf("Write INFO: %v", err)
	}
	if n != 0 {
		t.Fatalf("INFO must be skipped by ERROR writer under simple mode, got n=%d", n)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	_ = w.file.Close()
}

func TestFileWriterAutoRotate(t *testing.T) {
	old := MaxSize
	MaxSize = 64
	defer func() { MaxSize = old }()

	dir := t.TempDir()
	w := newFileWriter(dir, ERROR)
	payload := []byte(strings.Repeat("z", 32) + "\n")
	if _, err := w.Write(ERROR, payload); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	// 第二次写入触发轮转; 同秒轮转通过序号后缀避免覆盖旧文件
	if _, err := w.Write(ERROR, payload); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	_ = w.file.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("auto rotate must create 2 files, got %d", len(entries))
	}
	// 两个文件均含完整 payload（旧文件未被覆盖）
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != string(payload) {
			t.Fatalf("file %s content = %q, want %q", e.Name(), data, payload)
		}
	}
}

func TestFileWriterRotateAcrossTime(t *testing.T) {
	dir := t.TempDir()
	w := newFileWriter(dir, ERROR)
	payload := []byte("rotate\n")
	if _, err := w.Write(ERROR, payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// 手动跨秒轮转
	if err := w.rotateFile(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("rotateFile: %v", err)
	}
	if _, err := w.Write(ERROR, payload); err != nil {
		t.Fatalf("Write after rotate: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	_ = w.file.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 files after rotate, got %d", len(entries))
	}
}

func TestFileWriterCreateError(t *testing.T) {
	// 修复后: create 失败时 Write 返回错误, Flush/Sync 不再 panic
	w := newFileWriter(filepath.Join(t.TempDir(), "missing"), INFO)
	n, err := w.Write(INFO, []byte("x"))
	if err == nil {
		t.Fatal("Write must fail when log dir is missing")
	}
	if n != 0 {
		t.Fatalf("Write n = %d, want 0", n)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush must not fail on uninitialized writer: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync must not fail on uninitialized writer: %v", err)
	}
}

func TestCreateSameSecondNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	f1, _, err := create(dir, "INFO", now)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f1.WriteString("first\n")
	_ = f1.Close()

	f2, _, err := create(dir, "INFO", now) // 同秒同名, 应生成序号后缀文件
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f2.WriteString("second\n")
	_ = f2.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("same-second create must not overwrite, got %d files", len(entries))
	}
	// 基础名文件保留 first 内容, 序号后缀文件写入 second
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "first") {
		t.Fatalf("base file content = %q, want first", data)
	}
	data, err = os.ReadFile(filepath.Join(dir, entries[1].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "second") {
		t.Fatalf("numbered file content = %q, want second", data)
	}
}

func TestFileWriterStripAnsi(t *testing.T) {
	dir := t.TempDir()
	w := newFileWriter(dir, INFO)
	_, err := w.Write(INFO, []byte("\x1b[42m[INFO]\x1b[0m hello\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	_ = w.file.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "\x1b") {
		t.Fatalf("file must not contain ANSI escapes: %q", s)
	}
	if !strings.Contains(s, "[INFO] hello") {
		t.Fatalf("file content = %q, want [INFO] hello", s)
	}
}

func TestHttpWriterPlaceholder(t *testing.T) {
	w := newHttpWriter("http://127.0.0.1:8080/log", ERROR)
	w.Compare(func(v1 Level, v2 Level) bool { return v1 < v2 })
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	// 当前为占位空实现: 数据被静默丢弃
	n, err := w.Write(ERROR, []byte("x"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 0 {
		t.Fatalf("placeholder writer must drop data, got n=%d", n)
	}
}
