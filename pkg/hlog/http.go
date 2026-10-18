package hlog

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// httpWriter 将日志缓冲后批量 POST 上传到指定 HTTP 地址,
// 发送失败时数据保留在内存中等待下次 Flush 重试。
type httpWriter struct {
	mu    sync.Mutex
	url   string
	level Level
	call  func(v1 Level, v2 Level) bool
	out   *bufio.Writer
	buf   *bytes.Buffer
}

// maxPendingSize 发送失败时保留待重发数据的上限, 超过则丢弃避免内存无限增长。
var maxPendingSize = 512 * 1024

// httpClient 上传日志用的 HTTP 客户端。
var httpClient = &http.Client{Timeout: 10 * time.Second}

func newHttpWriter(url string, level Level) *httpWriter {
	buf := &bytes.Buffer{}
	return &httpWriter{
		url:   url,
		level: level,
		call:  func(v1 Level, v2 Level) bool { return v1 <= v2 },
		out:   bufio.NewWriterSize(buf, bufferSize),
		buf:   buf,
	}
}

func (h *httpWriter) Compare(call func(v1 Level, v2 Level) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if call != nil {
		h.call = call
	}
}

func (h *httpWriter) Flush() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.out.Flush(); err != nil {
		return err
	}
	if h.buf.Len() == 0 {
		return nil
	}
	err := h.sendLocked()
	if err != nil {
		// 发送失败保留数据等待重试, 超过上限则丢弃
		if h.buf.Len() > maxPendingSize {
			h.buf.Reset()
		}
		return err
	}
	return nil
}

func (h *httpWriter) Sync() error {
	return h.Flush()
}

// Write 中 call 谓词语义: 返回 true 表示该级别应写入此 writer(见 Logger.setSimple)。
func (h *httpWriter) Write(level Level, p []byte) (n int, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.call(level, h.level) {
		return 0, nil
	}
	if StripAnsi {
		p = stripAnsi(p)
	}
	// 缓冲将超限时先尝试发送
	if h.out.Buffered()+len(p) > bufferSize {
		if err := h.out.Flush(); err != nil {
			return 0, err
		}
		if err := h.sendLocked(); err != nil {
			if h.buf.Len() > maxPendingSize {
				h.buf.Reset()
			}
		}
	}
	return h.out.Write(p)
}

func (h *httpWriter) sendLocked() error {
	if h.buf.Len() == 0 {
		return nil
	}
	// 拷贝后发送: 失败时保留原始数据以便重试
	body := append([]byte(nil), h.buf.Bytes()...)
	req, err := http.NewRequest(http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := httpClient.Do(req)
	if err != nil {
		// 网络错误(如超时)后连接状态未知, 主动回收 idle 连接避免悬挂连接泄漏
		httpClient.CloseIdleConnections()
		_, _ = fmt.Fprintf(os.Stderr, "hlog: upload to %s failed: %v\n", h.url, err)
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = fmt.Fprintf(os.Stderr, "hlog: upload to %s failed, status %s\n", h.url, resp.Status)
		return fmt.Errorf("hlog: upload to %s failed, status %s", h.url, resp.Status)
	}
	h.buf.Reset()
	return nil
}
