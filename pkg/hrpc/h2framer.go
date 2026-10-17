package hrpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// 流量控制与帧大小默认值
const (
	initialConnWindow      = 65535    // RFC 7540 连接级初始窗口
	initialStreamWindow    = 65535    // 对端流初始窗口默认值
	recvStreamWindow       = 1 << 20  // 本端通告的流接收窗口
	recvMaxFrameSize       = 1 << 20  // 本端通告的最大帧大小
	connWindowLowWaterMark = 32 << 10 // 连接接收窗口低水位
	defaultMaxBatchBytes   = 32 << 10 // 合并缓冲默认上限
	defaultFramerTimeout   = 5 * time.Second
	defaultSpinDuration    = 100 * time.Microsecond // 写循环空闲自旋时长

	absorbWindowInit = 5 * time.Microsecond   // 流帧批次吸收窗口初始值,单请求额外延迟可忽略
	absorbWindowMax  = 512 * time.Microsecond // 流帧批次吸收窗口上限

	controlAbsorbInit = 256 * time.Microsecond // 纯控制帧批次吸收窗口初始值
	controlAbsorbMax  = 2 * time.Millisecond   // 纯控制帧批次吸收窗口上限
)

var errIdleTimeout = errors.New("hrpc: h2 connection idle timeout")

// H2FramerTransportOption H2FramerTransport 的选项。
type H2FramerTransportOption func(*H2FramerTransport)

// WithFramerTLSConfig 设置 TLS 配置(https 时生效)
func WithFramerTLSConfig(tlsConfig *tls.Config) H2FramerTransportOption {
	return func(t *H2FramerTransport) {
		t.tlsConfig = tlsConfig
	}
}

// WithFramerInsecureSkipVerify 跳过 TLS 证书校验(仅建议测试环境使用)
func WithFramerInsecureSkipVerify(insecureSkipVerify bool) H2FramerTransportOption {
	return func(t *H2FramerTransport) {
		if t.tlsConfig == nil {
			t.tlsConfig = &tls.Config{}
		}
		t.tlsConfig.InsecureSkipVerify = insecureSkipVerify
	}
}

// WithFramerDialTimeout 设置连接超时时间
func WithFramerDialTimeout(timeout time.Duration) H2FramerTransportOption {
	return func(t *H2FramerTransport) {
		t.dialTimeout = timeout
	}
}

// WithFramerIdleConnTimeout 设置空闲连接保活时间,0 表示不限
func WithFramerIdleConnTimeout(timeout time.Duration) H2FramerTransportOption {
	return func(t *H2FramerTransport) {
		t.idleTimeout = timeout
	}
}

// WithFramerMaxBatchBytes 设置合并缓冲上限。
// 写循环在缓冲达到该大小时强制 flush,避免大批量帧阻塞小请求。
func WithFramerMaxBatchBytes(size int) H2FramerTransportOption {
	return func(t *H2FramerTransport) {
		if size > 0 {
			t.maxBatchBytes = size
		}
	}
}

// WithFramerDialContext 自定义拨号函数(测试注入用)
func WithFramerDialContext(dial func(ctx context.Context, network, addr string) (net.Conn, error)) H2FramerTransportOption {
	return func(t *H2FramerTransport) {
		t.dialContext = dial
	}
}

// NewH2FramerTransport 创建一个基于 x/net/http2 公开 Framer 的客户端传输。
//
// 该传输在单条 h2 连接上仿照 gRPC loopyWriter 的帧合并策略:
//   - 所有并发请求的 HEADERS+DATA 帧先写入同一缓冲区;
//   - 单个写协程在队列耗尽且吸收窗口无新条目或缓冲达到上限时,
//     将合并后的字节一次写 TCP,将高并发下 N 个流 2N 次 syscall 收敛为少数几次批量写;
//   - 读协程用 ReadMetaHeaders 合并 HEADERS/CONTINUATION,并维护双向流控。
//
// 仅支持 http(h2c) 与 https(ALPN h2) 两种 scheme。
func NewH2FramerTransport(options ...H2FramerTransportOption) *H2FramerTransport {
	ret := &H2FramerTransport{
		tlsConfig:     &tls.Config{},
		dialTimeout:   defaultFramerTimeout,
		maxBatchBytes: defaultMaxBatchBytes,
		spinDuration:  defaultSpinDuration,
	}
	ret.dialContext = (&net.Dialer{
		Timeout:   ret.dialTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	for _, option := range options {
		option(ret)
	}
	return ret
}

// H2FramerTransport 基于 Framer 的 h2 客户端传输,实现 http.RoundTripper。
type H2FramerTransport struct {
	tlsConfig     *tls.Config
	dialTimeout   time.Duration
	idleTimeout   time.Duration
	maxBatchBytes int
	spinDuration  time.Duration
	dialContext   func(ctx context.Context, network, addr string) (net.Conn, error)

	mu   sync.Mutex
	conn *h2ClientConn
}

// RoundTrip 实现 http.RoundTripper。
func (t *H2FramerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		_ = req.Body.Close()
		return nil, err
	}
	_ = req.Body.Close()

	cc, err := t.getConn(ctx, req.URL)
	if err != nil {
		return nil, err
	}

	st := cc.newStream(req, body)
	cc.queue.push(&writeItem{st: st})

	select {
	case <-st.headDone:
	case <-ctx.Done():
		st.canceled.Store(true)
		cc.resetStream(st.id, http2.ErrCodeCancel)
		st.fail(ctx.Err())
		return nil, ctx.Err()
	case <-cc.done:
		return nil, cc.connErr()
	}

	st.mu.Lock()
	status, header, respErr := st.status, st.header, st.respErr
	st.mu.Unlock()
	if respErr != nil {
		return nil, respErr
	}
	resp := &http.Response{
		Status:        strconv.Itoa(status) + " " + http.StatusText(status),
		StatusCode:    status,
		Header:        header,
		Proto:         "HTTP/2.0",
		ProtoMajor:    2,
		Body:          &framerBody{st: st},
		ContentLength: -1,
		Request:       req,
	}
	go cc.watchCancel(ctx, st)
	return resp, nil
}

// CloseIdleConnections 关闭当前空闲连接。
func (t *H2FramerTransport) CloseIdleConnections() {
	t.mu.Lock()
	cc := t.conn
	t.conn = nil
	t.mu.Unlock()
	if cc != nil {
		cc.shutdown(errors.New("hrpc: idle connections closed"))
	}
}

// getConn 返回可复用的连接,空闲超时或已关闭时重建。
// 拨号在锁内完成(singleflight):并发请求同时 miss 时只有一个执行 dial,
// 其余复用新连接,避免拨号风暴与胜者替换的浪费。
func (t *H2FramerTransport) getConn(ctx context.Context, u *url.URL) (*h2ClientConn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cc := t.conn
	if cc != nil {
		select {
		case <-cc.done:
			t.conn = nil
		default:
			if t.idleTimeout > 0 && time.Since(time.Unix(0, cc.lastActive.Load())) > t.idleTimeout {
				cc.shutdown(errIdleTimeout)
				t.conn = nil
			}
		}
	}
	if t.conn != nil {
		return t.conn, nil
	}

	nc, err := t.dial(ctx, u)
	if err != nil {
		return nil, err
	}
	cc = newH2ClientConn(t, nc)
	if err := cc.start(); err != nil {
		return nil, err
	}
	t.conn = cc
	return cc, nil
}

// dial 建立 TCP 连接,https 时完成 ALPN h2 握手。
func (t *H2FramerTransport) dial(ctx context.Context, u *url.URL) (net.Conn, error) {
	conn, err := t.dialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" {
		return conn, nil
	}
	cfg := t.tlsConfig
	if cfg == nil {
		cfg = &tls.Config{}
	}
	cfg = cfg.Clone()
	cfg.NextProtos = []string{http2.NextProtoTLS}
	tc := tls.Client(conn, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if tc.ConnectionState().NegotiatedProtocol != http2.NextProtoTLS {
		_ = tc.Close()
		return nil, errors.New("hrpc: server did not negotiate h2")
	}
	return tc, nil
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// h2ClientConn 单条 h2 客户端连接。
type h2ClientConn struct {
	t          *H2FramerTransport
	nc         net.Conn
	queue      *writeQueue[*writeItem]
	done       chan struct{}
	readFramer *http2.Framer
	streams    sync.Map
	nextStream atomic.Uint32

	peerMaxFrame   atomic.Uint32
	peerInitialWin atomic.Int32
	connWin        atomic.Int32
	connRecvWin    atomic.Int32
	lastActive     atomic.Int64

	winUpdMu     sync.Mutex
	winUpd       map[uint32]uint32 // 待发 WINDOW_UPDATE 增量,streamID 0 为连接级
	winUpdQueued atomic.Bool       // 合并条目是否已入队

	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

// newH2ClientConn 创建连接对象。读侧 Framer 启用 MetaHeadersFrame 合并。
func newH2ClientConn(t *H2FramerTransport, nc net.Conn) *h2ClientConn {
	cc := &h2ClientConn{
		t:          t,
		nc:         nc,
		queue:      newWriteQueue[*writeItem](),
		done:       make(chan struct{}),
		readFramer: http2.NewFramer(io.Discard, nc),
	}
	cc.readFramer.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	cc.peerMaxFrame.Store(16384)
	cc.peerInitialWin.Store(initialStreamWindow)
	cc.connWin.Store(initialConnWindow)
	cc.connRecvWin.Store(initialConnWindow)
	cc.lastActive.Store(time.Now().UnixNano())
	return cc
}

// start 发送客户端前言与 SETTINGS,并启动读写协程。
func (cc *h2ClientConn) start() error {
	var buf bytes.Buffer
	buf.WriteString(http2.ClientPreface)
	fr := http2.NewFramer(&buf, nil)
	_ = fr.WriteSettings(
		http2.Setting{ID: http2.SettingEnablePush, Val: 0},
		http2.Setting{ID: http2.SettingInitialWindowSize, Val: recvStreamWindow},
		http2.Setting{ID: http2.SettingMaxFrameSize, Val: recvMaxFrameSize},
	)
	if err := writeAll(cc.nc, buf.Bytes()); err != nil {
		_ = cc.nc.Close()
		return err
	}
	go cc.readLoop()
	go cc.writeLoop()
	if cc.t.idleTimeout > 0 {
		go cc.idleTimer()
	}
	return nil
}

// stream 按流 ID 查找流。
func (cc *h2ClientConn) stream(id uint32) (*clientStream, bool) {
	v, ok := cc.streams.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*clientStream), true
}

// newStream 分配流 ID 并登记流。请求头块由调用方预编码(gRPC 同款:
// caller 编码 header,写协程只做帧序列化),提交路径只做原子分配与入队,
// 保证高并发下队列快速积累。
func (cc *h2ClientConn) newStream(req *http.Request, body []byte) *clientStream {
	id := cc.nextStream.Add(2) - 1 // 客户端流 ID 为奇数:1,3,5...
	st := &clientStream{
		id:         id,
		cc:         cc,
		req:        req,
		body:       body,
		headerFrag: encodeRequestHeaders(req),
		endStream:  true,
		headDone:   make(chan struct{}),
		finishCh:   make(chan struct{}),
		chunks:     make(chan []byte, 64),
		errCh:      make(chan error, 1),
	}
	st.sendWin.Store(cc.peerInitialWin.Load())
	cc.streams.Store(id, st)
	return st
}

// watchCancel 在请求生命周期内监听 ctx 取消,取消时发 RST_STREAM。
func (cc *h2ClientConn) watchCancel(ctx context.Context, st *clientStream) {
	select {
	case <-ctx.Done():
		if !st.finished.Load() && !st.aborted.Load() {
			st.canceled.Store(true)
			cc.resetStream(st.id, http2.ErrCodeCancel)
			st.fail(ctx.Err())
		}
	case <-st.finishCh:
	}
}

// resetStream 入队 RST_STREAM 帧,由写循环批量发出。
func (cc *h2ClientConn) resetStream(id uint32, code http2.ErrCode) {
	cc.queue.push(&writeItem{control: func(fr *http2.Framer) {
		_ = fr.WriteRSTStream(id, code)
	}})
}

// addWindowUpdate 合并回补接收窗口:增量攒入 map,只入队一个合并条目,
// 由写循环一次性批量写出,避免每个响应 DATA 单独触发一次 flush。
func (cc *h2ClientConn) addWindowUpdate(streamID uint32, incr uint32) {
	cc.winUpdMu.Lock()
	if cc.winUpd == nil {
		cc.winUpd = make(map[uint32]uint32)
	}
	cc.winUpd[streamID] += incr
	cc.winUpdMu.Unlock()
	if cc.winUpdQueued.CompareAndSwap(false, true) {
		cc.queue.push(&writeItem{control: cc.writeWindowUpdates})
	}
}

// writeWindowUpdates 取出合并增量并批量写 WINDOW_UPDATE。仅写协程调用。
// 取增量与置 false 必须在同一锁内原子完成,否则两者之间新增的增量
// 会 CAS 失败不 push,导致连接级窗口回补被遗留而流控停滞。
func (cc *h2ClientConn) writeWindowUpdates(fr *http2.Framer) {
	cc.winUpdMu.Lock()
	m := cc.winUpd
	cc.winUpd = nil
	cc.winUpdQueued.Store(false)
	cc.winUpdMu.Unlock()
	for id, incr := range m {
		_ = fr.WriteWindowUpdate(id, incr)
	}
}

// shutdown 关闭连接并失败所有在途流。幂等。
func (cc *h2ClientConn) shutdown(err error) {
	cc.closeOnce.Do(func() {
		cc.errMu.Lock()
		cc.err = err
		cc.errMu.Unlock()
		close(cc.done)
		_ = cc.nc.Close()
	})
	cc.failAll()
}

// failAll 失败所有在途流。
func (cc *h2ClientConn) failAll() {
	cc.streams.Range(func(_, v any) bool {
		st := v.(*clientStream)
		st.fail(cc.connErr())
		st.finish()
		return true
	})
}

// connErr 返回连接关闭原因。
func (cc *h2ClientConn) connErr() error {
	cc.errMu.Lock()
	defer cc.errMu.Unlock()
	if cc.err != nil {
		return cc.err
	}
	return errors.New("hrpc: h2 connection closed")
}

// isClosed 判断连接是否已关闭。
func (cc *h2ClientConn) isClosed() bool {
	select {
	case <-cc.done:
		return true
	default:
		return false
	}
}

// idleTimer 空闲超时后关闭连接。
func (cc *h2ClientConn) idleTimer() {
	interval := cc.t.idleTimeout / 2
	if interval <= 0 {
		interval = cc.t.idleTimeout
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if time.Since(time.Unix(0, cc.lastActive.Load())) > cc.t.idleTimeout {
				cc.shutdown(errIdleTimeout)
				return
			}
		case <-cc.done:
			return
		}
	}
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
// 读协程

// readLoop 读取帧并分发。流控窗口回补帧同样入队批量发出。
func (cc *h2ClientConn) readLoop() {
	ctx := hlog.NewContext()
	for {
		cc.lastActive.Store(time.Now().UnixNano())
		f, err := cc.readFramer.ReadFrame()
		if err != nil {
			if !cc.isClosed() {
				hlog.Warn(ctx, "h2 read loop exit: %v", err)
				cc.shutdown(err)
			}
			return
		}
		switch f := f.(type) {
		case *http2.SettingsFrame:
			cc.handleSettings(f)
		case *http2.PingFrame:
			if !f.IsAck() {
				data := f.Data
				cc.queue.push(&writeItem{control: func(fr *http2.Framer) {
					_ = fr.WritePing(true, data)
				}})
			}
		case *http2.WindowUpdateFrame:
			cc.handleWindowUpdate(f)
		case *http2.MetaHeadersFrame:
			cc.handleHeaders(f)
		case *http2.DataFrame:
			cc.handleData(f)
		case *http2.RSTStreamFrame:
			if st, ok := cc.stream(f.StreamID); ok {
				st.fail(http2.StreamError{StreamID: f.StreamID, Code: f.ErrCode})
				st.finish()
			}
		case *http2.GoAwayFrame:
			cc.shutdown(fmt.Errorf("hrpc: server sent GOAWAY: %v", f.ErrCode))
			return
		}
	}
}

// handleSettings 处理服务端 SETTINGS:更新对端最大帧与流窗口,回 ACK。
func (cc *h2ClientConn) handleSettings(f *http2.SettingsFrame) {
	if f.IsAck() {
		return
	}
	_ = f.ForeachSetting(func(s http2.Setting) error {
		switch s.ID {
		case http2.SettingMaxFrameSize:
			if s.Val >= 16384 && s.Val <= 1<<24-1 {
				cc.peerMaxFrame.Store(s.Val)
			}
		case http2.SettingInitialWindowSize:
			old := cc.peerInitialWin.Load()
			if old != int32(s.Val) {
				delta := int32(s.Val) - old
				cc.peerInitialWin.Store(int32(s.Val))
				cc.streams.Range(func(_, v any) bool {
					v.(*clientStream).sendWin.Add(delta)
					return true
				})
				cc.queue.notify()
			}
		}
		return nil
	})
	cc.queue.push(&writeItem{control: func(fr *http2.Framer) {
		_ = fr.WriteSettingsAck()
	}})
}

// handleWindowUpdate 更新发送窗口并唤醒写循环。
func (cc *h2ClientConn) handleWindowUpdate(f *http2.WindowUpdateFrame) {
	incr := int32(f.Increment)
	if f.StreamID == 0 {
		cc.connWin.Add(incr)
	} else if st, ok := cc.stream(f.StreamID); ok {
		st.sendWin.Add(incr)
	}
	cc.queue.notify()
}

// handleHeaders 处理响应头,唤醒等待响应的 RoundTrip。
func (cc *h2ClientConn) handleHeaders(f *http2.MetaHeadersFrame) {
	st, ok := cc.stream(f.StreamID)
	if !ok {
		cc.resetStream(f.StreamID, http2.ErrCodeStreamClosed)
		return
	}
	code, _ := strconv.Atoi(f.PseudoValue("status"))
	if code == 0 {
		code = http.StatusServiceUnavailable
	}
	header := http.Header{}
	for _, hf := range f.RegularFields() {
		header.Add(hf.Name, hf.Value)
	}
	st.mu.Lock()
	st.status = code
	st.header = header
	st.mu.Unlock()
	st.headOnce.Do(func() { close(st.headDone) })
	if f.StreamEnded() {
		st.finish()
	}
}

// handleData 处理响应 DATA:投递数据并回补流控窗口。
func (cc *h2ClientConn) handleData(f *http2.DataFrame) {
	size := len(f.Data())
	if size > 0 {
		cc.connRecvWin.Add(-int32(size))
		connIncr := uint32(0)
		if cc.connRecvWin.Load() < connWindowLowWaterMark {
			connIncr = uint32(initialConnWindow - cc.connRecvWin.Load())
			cc.connRecvWin.Add(int32(connIncr))
		}
		st, ok := cc.stream(f.StreamID)
		switch {
		case ok && !st.aborted.Load():
			chunk := make([]byte, size)
			copy(chunk, f.Data())
			st.deliver(chunk)
			cc.addWindowUpdate(f.StreamID, uint32(size))
		case !ok:
			cc.resetStream(f.StreamID, http2.ErrCodeStreamClosed)
		}
		if connIncr > 0 {
			cc.addWindowUpdate(0, connIncr)
		}
	}
	if f.StreamEnded() {
		if st, ok := cc.stream(f.StreamID); ok {
			st.finish()
		}
	}
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
// 写协程(仿 gRPC loopyWriter)

// writeItem 写队列条目:流帧或控制帧闭包。
type writeItem struct {
	st      *clientStream
	control func(fr *http2.Framer)
}

// writeLoop 唯一写协程:批量合并所有流的帧后一次 flush 到 TCP。
// 并发入队的流 ID 可能乱序,新流 HEADERS 需等待 maxOpened+2 才可写,
// 保证线上流 ID 单调递增(RFC 7540)。
// flush 时机:队列耗尽且吸收窗口内无新条目(保证低延迟),写入速率高于消费时
// 队列持续非空,帧不断并入同一缓冲区,直到达到 maxBatchBytes 才强制 flush。
func (cc *h2ClientConn) writeLoop() {
	var wbuf bytes.Buffer
	fr := http2.NewFramer(&wbuf, nil)
	var leftovers []*writeItem
	var maxOpened int64 = -1                      // 已发出 HEADERS 的最大流 ID,首流 ID 为 1
	var streamWindow, controlWindow time.Duration // 流帧/控制帧批次各自独立窗口
	hasStream := false                            // 当前缓冲是否含流帧,决定吸收窗口档位
	for {
		batch := cc.queue.popAll()
		if len(leftovers) > 0 {
			batch = append(leftovers, batch...)
			leftovers = leftovers[:0]
		}
		if len(batch) == 0 {
			if wbuf.Len() == 0 {
				// 队列空时先短暂自旋,保持写循环热运行:串行/低并发场景
				// 下一请求 push 后无需跨 M 唤醒,避免 park/unpark 调度延迟。
				if cc.queue.spinWait(cc.t.spinDuration, cc.done) {
					continue
				}
				select {
				case <-cc.queue.wake:
				case <-cc.done:
					return
				}
				continue
			}
			// 队列暂时耗尽:进入吸收窗口,把窗口期内新到达的条目并入本批,
			// 避免并发请求逐个 flush 造成每请求一次 TCP 写。
			// 流帧与控制帧批次各用独立窗口,避免互相污染窗口大小。
			if hasStream {
				if cc.absorb(&streamWindow, true) {
					continue
				}
			} else if cc.absorb(&controlWindow, false) {
				continue
			}
			// 吸收窗口内无新条目,flush 合并缓冲
			cc.lastActive.Store(time.Now().UnixNano())
			if err := writeAll(cc.nc, wbuf.Bytes()); err != nil {
				wbuf.Reset()
				cc.shutdown(err)
				return
			}
			wbuf.Reset()
			streamWindow, controlWindow = 0, 0 // flush 后窗口归零,下次突发重新成长
			hasStream = false
			continue
		}
		hasStream = false
		lastOpened := maxOpened
		for i, item := range batch {
			if item.control != nil {
				item.control(fr)
				continue
			}
			hasStream = true
			st := item.st
			if !st.headerWritten {
				if int64(st.id) != maxOpened+2 {
					// 乱序到达,延后等待前面的流开启
					leftovers = append(leftovers, item)
					continue
				}
				if st.canceled.Load() || st.aborted.Load() {
					// 流未开启即取消,跳过并推进顺序窗口
					maxOpened = int64(st.id)
					continue
				}
				cc.writeHeaderBlock(fr, st)
				st.headerWritten = true
				maxOpened = int64(st.id)
			}
			if cc.writeData(fr, st) {
				leftovers = append(leftovers, item)
			}
			if wbuf.Len() >= cc.t.maxBatchBytes {
				// 批量达到上限,未处理的条目延后到下一批
				leftovers = append(leftovers, batch[i+1:]...)
				cc.lastActive.Store(time.Now().UnixNano())
				if err := writeAll(cc.nc, wbuf.Bytes()); err != nil {
					wbuf.Reset()
					cc.shutdown(err)
					return
				}
				wbuf.Reset()
				break
			}
		}
		if len(leftovers) > 0 {
			if maxOpened != lastOpened {
				// 本批推进了流顺序,乱序延后条目立即可重试
				continue
			}
			// 无进展:流控阻塞等 WINDOW_UPDATE,或等前序流 push 唤醒
			if wbuf.Len() > 0 {
				cc.lastActive.Store(time.Now().UnixNano())
				if err := writeAll(cc.nc, wbuf.Bytes()); err != nil {
					wbuf.Reset()
					cc.shutdown(err)
					return
				}
				wbuf.Reset()
			}
			select {
			case <-cc.queue.wake:
			case <-cc.done:
				return
			}
			continue
		}
		if wbuf.Len() == 0 {
			// 无阻塞且无缓冲,等待新条目唤醒
			select {
			case <-cc.queue.wake:
			case <-cc.done:
				return
			}
		}
		// 队列仍有积压,继续并入本批
	}
}

// absorb 队列暂时为空时进入吸收窗口,返回窗口内是否有新条目到达。
// 窗口随命中指数增长、空等减半(Nagle 式)。流帧批次用低延迟小窗口,
// 纯控制帧批次(如响应触发的 WINDOW_UPDATE)用大窗口,把长时间内
// 交错到达的控制帧并入同一批 flush。
func (cc *h2ClientConn) absorb(window *time.Duration, hasStream bool) bool {
	init, max := absorbWindowInit, absorbWindowMax
	if !hasStream {
		init, max = controlAbsorbInit, controlAbsorbMax
	}
	if *window <= 0 {
		*window = init
	} else if *window > max {
		*window = max
	}
	timer := time.NewTimer(*window)
	defer timer.Stop()
	select {
	case <-cc.queue.wake:
		*window = min(*window*2, max)
		return true
	case <-cc.done:
		return false
	case <-timer.C:
		*window /= 2
		return false
	}
}

// writeData 写入流的 DATA 帧,返回是否有数据因流控未写完。
func (cc *h2ClientConn) writeData(fr *http2.Framer, st *clientStream) bool {
	maxFrame := int32(cc.peerMaxFrame.Load())
	for st.bodyOff < len(st.body) {
		n := int32(len(st.body) - st.bodyOff)
		if n > maxFrame {
			n = maxFrame
		}
		if connWin := cc.connWin.Load(); n > connWin {
			n = connWin
		}
		if streamWin := st.sendWin.Load(); n > streamWin {
			n = streamWin
		}
		if n <= 0 {
			return true // 窗口不足,等待 WINDOW_UPDATE
		}
		cc.connWin.Add(-n)
		st.sendWin.Add(-n)
		end := st.bodyOff + int(n)
		_ = fr.WriteData(st.id, end == len(st.body) && st.endStream, st.body[st.bodyOff:end])
		st.bodyOff = end
	}
	return false
}

// writeHeaderBlock 用调用方预编码的头块写入 HEADERS 帧,按对端最大帧拆分 CONTINUATION。
// body 为空时 HEADERS 直接携带 END_STREAM。
func (cc *h2ClientConn) writeHeaderBlock(fr *http2.Framer, st *clientStream) {
	frag := st.headerFrag
	st.headerFrag = nil // 头块已序列化,释放内存
	maxFrame := int(cc.peerMaxFrame.Load())
	endStream := st.endStream && len(st.body) == 0
	n := len(frag)
	if n > maxFrame {
		n = maxFrame
	}
	_ = fr.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      st.id,
		BlockFragment: frag[:n],
		EndStream:     endStream,
		EndHeaders:    n == len(frag),
	})
	frag = frag[n:]
	for len(frag) > 0 {
		n = len(frag)
		if n > maxFrame {
			n = maxFrame
		}
		_ = fr.WriteContinuation(st.id, n == len(frag), frag[:n])
		frag = frag[n:]
	}
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// clientStream 单个请求流。
type clientStream struct {
	id            uint32
	cc            *h2ClientConn
	req           *http.Request
	body          []byte
	bodyOff       int
	endStream     bool
	headerWritten bool
	sendWin       atomic.Int32
	canceled      atomic.Bool
	aborted       atomic.Bool
	finished      atomic.Bool

	headOnce   sync.Once
	headDone   chan struct{}
	failOnce   sync.Once
	finishOnce sync.Once
	finishCh   chan struct{}

	mu      sync.Mutex
	status  int
	header  http.Header
	respErr error

	chunks chan []byte
	errCh  chan error
	cur    []byte

	headerFrag []byte // 调用方预编码的 HPACK 头块
}

// fail 以错误终结流:唤醒等待头部的 RoundTrip 与等待数据的 Body 读者。
func (st *clientStream) fail(err error) {
	st.failOnce.Do(func() {
		st.mu.Lock()
		st.respErr = err
		st.mu.Unlock()
		select {
		case st.errCh <- err:
		default:
		}
	})
	st.headOnce.Do(func() { close(st.headDone) })
}

// finish 正常终结流:关闭数据通道并从连接中移除。仅读协程调用。
func (st *clientStream) finish() {
	st.finishOnce.Do(func() {
		st.finished.Store(true)
		close(st.chunks)
		close(st.finishCh)
		st.cc.streams.Delete(st.id)
	})
}

// deliver 投递响应数据块,连接关闭时退出。
func (st *clientStream) deliver(chunk []byte) {
	select {
	case st.chunks <- chunk:
	case <-st.cc.done:
	case <-st.finishCh:
	}
}

// framerBody 响应体,按块消费服务端 DATA。
type framerBody struct {
	st *clientStream
}

func (b *framerBody) Read(p []byte) (int, error) {
	st := b.st
	for {
		if len(st.cur) > 0 {
			n := copy(p, st.cur)
			st.cur = st.cur[n:]
			return n, nil
		}
		select {
		case chunk, ok := <-st.chunks:
			if !ok {
				return 0, io.EOF
			}
			st.cur = chunk
		case err := <-st.errCh:
			return 0, err
		case <-st.cc.done:
			return 0, st.cc.connErr()
		}
	}
}

// Close 提前关闭响应体:发 RST_STREAM 通知服务端停止发送。
func (b *framerBody) Close() error {
	st := b.st
	if st.finished.Load() || st.aborted.Load() {
		return nil
	}
	st.aborted.Store(true)
	st.fail(errors.New("hrpc: response body closed"))
	st.cc.resetStream(st.id, http2.ErrCodeCancel)
	return nil
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// writeQueue 写队列:批量出队,单元素唤醒通道。
// 泛型化以同时服务于客户端(h2ClientConn)与服务端(h2ServerConn)的写循环。
type writeQueue[T any] struct {
	mu   sync.Mutex
	q    []T
	wake chan struct{}
}

func newWriteQueue[T any]() *writeQueue[T] {
	return &writeQueue[T]{wake: make(chan struct{}, 1)}
}

func (q *writeQueue[T]) push(item T) {
	q.mu.Lock()
	q.q = append(q.q, item)
	q.mu.Unlock()
	q.notify()
}

// notify 非阻塞唤醒写循环,用于窗口更新等无新条目的场景。
func (q *writeQueue[T]) notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// spinWait 自旋等待新条目到达或超时,返回是否有新条目。
// 写循环空闲时保持热运行,避免串行场景下每请求一次跨 M 唤醒。
func (q *writeQueue[T]) spinWait(d time.Duration, done <-chan struct{}) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		has := len(q.q) > 0
		q.mu.Unlock()
		if has {
			return true
		}
		select {
		case <-done:
			return false
		default:
		}
		runtime.Gosched()
	}
	return false
}

func (q *writeQueue[T]) popAll() []T {
	q.mu.Lock()
	batch := q.q
	q.q = nil
	q.mu.Unlock()
	return batch
}

// encodeRequestHeaders 用 HPACK 静态表编码请求头块。
// 由调用方 goroutine 调用并发执行,写协程直接序列化已编码头块;
// 不使用动态表,解码端无需维护表状态。
func encodeRequestHeaders(req *http.Request) []byte {
	var buf bytes.Buffer
	enc := hpack.NewEncoder(&buf)
	scheme := req.URL.Scheme
	if scheme == "" {
		scheme = "https"
	}
	authority := req.URL.Host
	if req.Host != "" {
		authority = req.Host
	}
	enc.WriteField(hpack.HeaderField{Name: ":method", Value: req.Method})
	enc.WriteField(hpack.HeaderField{Name: ":scheme", Value: scheme})
	enc.WriteField(hpack.HeaderField{Name: ":authority", Value: authority})
	enc.WriteField(hpack.HeaderField{Name: ":path", Value: req.URL.RequestURI()})
	for k, vs := range req.Header {
		k = strings.ToLower(k)
		for _, v := range vs {
			enc.WriteField(hpack.HeaderField{Name: k, Value: v})
		}
	}
	return buf.Bytes()
}

// writeAll 完整写出 p。
func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}
