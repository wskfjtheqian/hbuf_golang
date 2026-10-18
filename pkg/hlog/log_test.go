package hlog

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// collectWriter 是测试用 SyncWriter 实现，记录收到的日志内容、条数与 Flush 次数。
type collectWriter struct {
	level      Level
	call       func(v1 Level, v2 Level) bool
	mu         sync.Mutex
	data       []byte
	count      int
	flushCount int
}

func newCollectWriter(level Level) *collectWriter {
	return &collectWriter{level: level}
}

func (w *collectWriter) Compare(call func(v1 Level, v2 Level) bool) { w.call = call }

func (w *collectWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushCount++
	return nil
}

func (w *collectWriter) Sync() error { return nil }

func (w *collectWriter) Write(level Level, p []byte) (n int, err error) {
	if w.call != nil && !w.call(level, w.level) {
		return 0, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.count++
	w.data = append(w.data, p...)
	return len(p), nil
}

func (w *collectWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.data)
}

func TestLevelString(t *testing.T) {
	want := map[Level]string{
		DEBUG: "DEBUG",
		INFO:  "INFO",
		WARN:  "WARN",
		ERROR: "ERROR",
		EXIT:  "EXIT",
	}
	for level, name := range want {
		if got := level.String(); got != name {
			t.Errorf("Level(%d).String() = %q, want %q", level, got, name)
		}
	}
	if got := Level(15000).String(); got != "15000" {
		t.Errorf("unknown level String() = %q, want %q", got, "15000")
	}
}

func TestSetLevelNameStandardProtected(t *testing.T) {
	// 标准级别名称受保护，传入的 name 会被忽略
	SetLevelName(INFO, "NOTICE")
	if got := INFO.String(); got != "INFO" {
		t.Fatalf("standard level name must be protected, got %q", got)
	}
}

func TestSetLevelNameCustom(t *testing.T) {
	// 保存并恢复全局 levelName, 避免污染其他测试
	levelNameLock.Lock()
	old := *levelName.Load()
	levelNameLock.Unlock()
	defer func() {
		levelNameLock.Lock()
		levelName.Store(&old)
		levelNameLock.Unlock()
	}()

	const custom Level = 15000
	SetLevelName(custom, "NOTICE")
	if got := custom.String(); got != "NOTICE" {
		t.Fatalf("custom level name = %q, want NOTICE", got)
	}
	SetLevelName(custom, "TRACE")
	if got := custom.String(); got != "TRACE" {
		t.Fatalf("custom level rename = %q, want TRACE", got)
	}
}

func TestLoggerDefaultOutLevel(t *testing.T) {
	l := NewLogger("", 0)
	defer l.Close()
	if got := l.GetOutLevel(); got != ERROR {
		t.Fatalf("default out level = %v, want ERROR", got)
	}
	l.SetOutLevel(DEBUG)
	if got := l.GetOutLevel(); got != DEBUG {
		t.Fatalf("out level after SetOutLevel = %v, want DEBUG", got)
	}
}

func TestLoggerPrefixAndFlags(t *testing.T) {
	l := NewLogger("a", LstdFlags)
	defer l.Close()
	if got := l.Prefix(); got != "a" {
		t.Fatalf("Prefix() = %q, want %q", got, "a")
	}
	if got := l.Flags(); got != LstdFlags {
		t.Fatalf("Flags() = %d, want %d", got, LstdFlags)
	}
	l.SetPrefix("b")
	l.SetFlags(Ldate)
	if got := l.Prefix(); got != "b" {
		t.Fatalf("Prefix() after SetPrefix = %q, want %q", got, "b")
	}
	if got := l.Flags(); got != Ldate {
		t.Fatalf("Flags() after SetFlags = %d, want %d", got, Ldate)
	}
}

func TestLoggerOutputLevelFilter(t *testing.T) {
	l := NewLogger("", 0)
	defer l.Close()
	l.setOutError(false)
	l.SetOutLevel(INFO)
	l.SetOutTarget(func(level Level) SyncWriter { return newCollectWriter(level) })

	// 低于输出级别的日志被直接丢弃，writer 不会被创建
	l.Debug("debug")
	if len(l.out) != 0 {
		t.Fatalf("below out level must not create writer, out=%v", l.out)
	}
	l.Info("info")
	if _, ok := l.out[INFO]; !ok {
		t.Fatal("INFO writer not created")
	}
}

func TestLoggerComplexRouting(t *testing.T) {
	// 默认复杂模式: 一条级别 L 的日志写入所有「文件级别 ≥ L」的 writer,
	// 即每个文件包含本级别及更低级别的日志。
	l := NewLogger("", 0)
	defer l.Close()
	l.setOutError(false)
	l.SetOutLevel(DEBUG)
	l.SetOutTarget(func(level Level) SyncWriter { return newCollectWriter(level) })

	l.Info("info-1")
	l.Error("error-1")
	l.Debug("debug-1")
	l.Info("info-2")

	if got := l.out[DEBUG].(*collectWriter).count; got != 1 {
		t.Fatalf("DEBUG writer count = %d, want 1", got)
	}
	if got := l.out[INFO].(*collectWriter).count; got != 3 {
		t.Fatalf("INFO writer count = %d, want 3", got)
	}
	if got := l.out[ERROR].(*collectWriter).count; got != 3 {
		t.Fatalf("ERROR writer count = %d, want 3", got)
	}
	// INFO 文件含 INFO 与更低级别的 DEBUG, 不含 ERROR
	s := l.out[INFO].(*collectWriter).String()
	for _, msg := range []string{"info-1", "info-2", "debug-1"} {
		if !strings.Contains(s, msg) {
			t.Fatalf("INFO writer missing %q: %q", msg, s)
		}
	}
	if strings.Contains(s, "error-1") {
		t.Fatalf("INFO writer must not contain %q: %q", "error-1", s)
	}
	// ERROR 文件含本级别与更低级别日志(不含创建前的 info-1)
	s = l.out[ERROR].(*collectWriter).String()
	for _, msg := range []string{"error-1", "info-2", "debug-1"} {
		if !strings.Contains(s, msg) {
			t.Fatalf("ERROR writer missing %q: %q", msg, s)
		}
	}
	if strings.Contains(s, "info-1") {
		t.Fatalf("ERROR writer must not contain logs written before its creation: %q", s)
	}
	// DEBUG 文件只含 DEBUG 日志
	s = l.out[DEBUG].(*collectWriter).String()
	if !strings.Contains(s, "debug-1") || strings.Contains(s, "info-1") || strings.Contains(s, "error-1") {
		t.Fatalf("DEBUG writer must contain only DEBUG logs: %q", s)
	}
}

func TestLoggerSimpleRouting(t *testing.T) {
	// 简单模式: 每个文件仅包含本级别的日志(与 -log_simple 描述一致)
	l := NewLogger("", 0)
	defer l.Close()
	l.setOutError(false)
	l.setSimple(true)
	l.SetOutLevel(DEBUG)
	l.SetOutTarget(func(level Level) SyncWriter { return newCollectWriter(level) })

	l.Error("error-1")
	l.Info("info-1")

	if got := l.out[ERROR].(*collectWriter).count; got != 1 {
		t.Fatalf("ERROR writer count = %d, want 1", got)
	}
	if got := l.out[INFO].(*collectWriter).count; got != 1 {
		t.Fatalf("INFO writer count = %d, want 1", got)
	}
	s := l.out[ERROR].(*collectWriter).String()
	if !strings.Contains(s, "error-1") || strings.Contains(s, "info-1") {
		t.Fatalf("ERROR writer must contain only ERROR logs: %q", s)
	}
	s = l.out[INFO].(*collectWriter).String()
	if !strings.Contains(s, "info-1") || strings.Contains(s, "error-1") {
		t.Fatalf("INFO writer must contain only INFO logs: %q", s)
	}
}

func TestLoggerOutputRaw(t *testing.T) {
	l := NewLogger("", 0)
	defer l.Close()
	l.setOutError(false)
	l.SetOutLevel(DEBUG)
	l.SetOutTarget(func(level Level) SyncWriter { return newCollectWriter(level) })

	if err := l.Output(0, INFO, "raw message\n"); err != nil {
		t.Fatalf("Output: %v", err)
	}
	if s := l.out[INFO].(*collectWriter).String(); !strings.Contains(s, "raw message") {
		t.Fatalf("raw output missing: %q", s)
	}
}

func TestLoggerFlushCallsWriters(t *testing.T) {
	l := NewLogger("", 0)
	defer l.Close()
	l.setOutError(false)
	l.SetOutLevel(DEBUG)
	l.SetOutTarget(func(level Level) SyncWriter { return newCollectWriter(level) })

	l.Info("x")
	if got := l.out[INFO].(*collectWriter).flushCount; got != 0 {
		t.Fatalf("flushCount before Flush = %d, want 0", got)
	}
	l.Flush()
	if got := l.out[INFO].(*collectWriter).flushCount; got != 1 {
		t.Fatalf("flushCount after Flush = %d, want 1", got)
	}
}

func TestLoggerExitPanics(t *testing.T) {
	l := NewLogger("", 0)
	defer l.Close()
	l.setOutError(false)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Exit must panic")
		}
	}()
	l.Exit("boom %d", 1)
}

func TestStdoutBufferedAndLevelFiltered(t *testing.T) {
	old := os.Stdout
	tmp, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = tmp
	defer func() {
		os.Stdout = old
		_ = tmp.Close()
	}()

	// stdout 写入经 bufio 缓冲; 低于输出级别的日志不再打印到控制台
	l := NewLogger("", 0)
	defer l.Close()

	l.Info("low-level") // 默认输出级别 ERROR, 被过滤
	l.Error("high-level %d", 1)
	l.Flush()

	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "low-level") {
		t.Fatalf("below out level must not be printed: %q", s)
	}
	if !strings.Contains(s, "high-level 1") {
		t.Fatalf("stdout missing message: %q", s)
	}
}

func TestLoggerClose(t *testing.T) {
	l := NewLogger("", 0)
	l.Close()
	l.Close() // 重复 Close 安全
}

func TestFormatHeader(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	var buf []byte
	formatHeader(&buf, now, "svc ", Ldate|Ltime|LUTC|Lshortfile, "/a/b/c.go", 42, INFO)
	s := string(buf)
	for _, want := range []string{"svc 2026/10/18 12:00:00", "[INFO]", "\x1b[0m", "c.go:42"} {
		if !strings.Contains(s, want) {
			t.Fatalf("header %q missing %q", s, want)
		}
	}
	if !strings.HasPrefix(s, "svc ") {
		t.Fatalf("prefix must be at head (Lmsgprefix unset): %q", s)
	}

	// WARN 使用黄底色, Lmsgprefix 将 prefix 移到消息之前
	var buf2 []byte
	formatHeader(&buf2, now, "svc ", Ldate|Ltime|LUTC|Lmsgprefix, "", 0, WARN)
	s2 := string(buf2)
	if !strings.Contains(s2, "\x1b[43m[WARN]") {
		t.Fatalf("WARN must use yellow background: %q", s2)
	}
	if !strings.HasSuffix(s2, "svc ") {
		t.Fatalf("prefix must be at tail (Lmsgprefix set): %q", s2)
	}
}

func TestBufferPool(t *testing.T) {
	// 小缓冲归还后 cap 保留, 再次取出时 len 归零
	small := getBuffer()
	*small = append(*small, "hello"...)
	putBuffer(small)
	got := getBuffer()
	if len(*got) != 0 {
		t.Fatalf("buffer not reset, len=%d", len(*got))
	}
	putBuffer(got)

	// 超过 64KB 的缓冲归还时被置 nil 丢弃(不依赖池中对象的获取顺序)
	big := getBuffer()
	*big = append(*big, make([]byte, 65<<10)...)
	if cap(*big) <= 64<<10 {
		t.Fatalf("precondition: cap=%d must exceed 64KB", cap(*big))
	}
	putBuffer(big)
	if cap(*big) != 0 {
		t.Fatalf("big buffer must be dropped from pool, cap=%d", cap(*big))
	}
}
