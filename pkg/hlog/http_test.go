package hlog

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// uploadCollector 收集上传请求的 body, 供测试断言。
type uploadCollector struct {
	mu   sync.Mutex
	body []byte
	code atomic.Int32
}

func (c *uploadCollector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.body = append(c.body, data...)
	c.mu.Unlock()
	w.WriteHeader(int(c.code.Load()))
}

func (c *uploadCollector) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.body)
}

func TestHttpWriterWriteAndFlush(t *testing.T) {
	collector := &uploadCollector{}
	collector.code.Store(http.StatusOK)
	srv := httptest.NewServer(collector)
	defer srv.Close()

	w := newHttpWriter(srv.URL, INFO)
	if _, err := w.Write(INFO, []byte("log-1\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := w.Write(INFO, []byte("log-2\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Flush 前不上传
	if collector.String() != "" {
		t.Fatalf("uploaded before Flush: %q", collector.String())
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	s := collector.String()
	if !strings.Contains(s, "log-1") || !strings.Contains(s, "log-2") {
		t.Fatalf("uploaded body = %q, want log-1 and log-2", s)
	}
	// Sync 等价于 Flush
	if _, err := w.Write(INFO, []byte("log-3\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !strings.Contains(collector.String(), "log-3") {
		t.Fatalf("uploaded body after Sync = %q, want log-3", collector.String())
	}
}

func TestHttpWriterCompare(t *testing.T) {
	collector := &uploadCollector{}
	collector.code.Store(http.StatusOK)
	srv := httptest.NewServer(collector)
	defer srv.Close()

	w := newHttpWriter(srv.URL, ERROR)
	// 默认复杂谓词: INFO <= ERROR → 写入
	if _, err := w.Write(INFO, []byte("info\n")); err != nil {
		t.Fatalf("Write INFO: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !strings.Contains(collector.String(), "info") {
		t.Fatalf("complex mode must write lower level: %q", collector.String())
	}
	// 简单谓词: 仅本级别写入
	w.Compare(func(v1 Level, v2 Level) bool { return v1 == v2 })
	n, err := w.Write(INFO, []byte("skip\n"))
	if err != nil {
		t.Fatalf("Write INFO: %v", err)
	}
	if n != 0 {
		t.Fatalf("simple mode must skip non-matching level, got n=%d", n)
	}
	if _, err := w.Write(ERROR, []byte("error\n")); err != nil {
		t.Fatalf("Write ERROR: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	s := collector.String()
	if !strings.Contains(s, "error") || strings.Contains(s, "skip") {
		t.Fatalf("simple mode routing wrong: %q", s)
	}
}

func TestHttpWriterStripAnsi(t *testing.T) {
	collector := &uploadCollector{}
	collector.code.Store(http.StatusOK)
	srv := httptest.NewServer(collector)
	defer srv.Close()

	w := newHttpWriter(srv.URL, INFO)
	_, err := w.Write(INFO, []byte("\x1b[42m[INFO]\x1b[0m hello\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	s := collector.String()
	if strings.Contains(s, "\x1b") {
		t.Fatalf("uploaded body must not contain ANSI escapes: %q", s)
	}
	if !strings.Contains(s, "[INFO] hello") {
		t.Fatalf("uploaded body = %q, want [INFO] hello", s)
	}
}

func TestHttpWriterKeepAnsiWhenDisabled(t *testing.T) {
	old := StripAnsi
	StripAnsi = false
	defer func() { StripAnsi = old }()

	collector := &uploadCollector{}
	collector.code.Store(http.StatusOK)
	srv := httptest.NewServer(collector)
	defer srv.Close()

	w := newHttpWriter(srv.URL, INFO)
	_, err := w.Write(INFO, []byte("\x1b[42m[INFO]\x1b[0m hello\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !strings.Contains(collector.String(), "\x1b") {
		t.Fatalf("StripAnsi=false must keep ANSI escapes: %q", collector.String())
	}
}

func TestHttpWriterUploadFailureKeepsData(t *testing.T) {
	collector := &uploadCollector{}
	collector.code.Store(http.StatusInternalServerError)
	srv := httptest.NewServer(collector)
	defer srv.Close()

	w := newHttpWriter(srv.URL, INFO)
	if _, err := w.Write(INFO, []byte("keep-me\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err == nil {
		t.Fatal("Flush must fail when server returns 500")
	}
	// 失败后数据保留, 服务恢复后重试成功
	collector.code.Store(http.StatusOK)
	if err := w.Flush(); err != nil {
		t.Fatalf("retry Flush: %v", err)
	}
	if !strings.Contains(collector.String(), "keep-me") {
		t.Fatalf("retry must upload kept data: %q", collector.String())
	}
}

func TestHttpWriterDiscardOverflow(t *testing.T) {
	old := maxPendingSize
	maxPendingSize = 1024
	defer func() { maxPendingSize = old }()

	collector := &uploadCollector{}
	collector.code.Store(http.StatusInternalServerError)
	srv := httptest.NewServer(collector)
	defer srv.Close()

	w := newHttpWriter(srv.URL, INFO)
	_, _ = w.Write(INFO, []byte(strings.Repeat("x", 2048)+"\n"))
	if err := w.Flush(); err == nil {
		t.Fatal("Flush must fail when server returns 500")
	}
	if w.buf.Len() != 0 {
		t.Fatalf("overflowing pending data must be discarded, len=%d", w.buf.Len())
	}
}

func TestHttpWriterClientTimeout(t *testing.T) {
	// 验证 httpClient.Timeout 生效: 服务端挂起超过客户端超时时间时 Flush 返回错误。
	// 注意: 客户端超时后连接可能不会立即关闭, 服务端 request context 未必触发 Done,
	// 因此 handler 需要自行退出, 否则 httptest.Server.Close 会等待活跃连接而卡住。
	oldClient := httpClient
	httpClient = &http.Client{Timeout: 100 * time.Millisecond}
	defer func() { httpClient = oldClient }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	w := newHttpWriter(srv.URL, INFO)
	_, _ = w.Write(INFO, []byte("timeout\n"))
	if err := w.Flush(); err == nil {
		t.Fatal("Flush must fail when server hangs")
	}
}
