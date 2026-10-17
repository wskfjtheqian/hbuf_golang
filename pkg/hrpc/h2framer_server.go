package hrpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// 服务端默认值
const (
	defaultMaxConcurrentStreams = 10000 // 并发流上限,高并发场景单连接承载全部流
	defaultServerIdleTimeout    = 5 * time.Minute
)

// H2ServerOption H2Server 的选项。
type H2ServerOption func(*H2Server)

// WithH2MaxConcurrentStreams 设置单连接最大并发流数。
func WithH2MaxConcurrentStreams(maxStreams uint32) H2ServerOption {
	return func(h *H2Server) {
		h.maxConcurrentStreams = maxStreams
	}
}

// WithH2IdleTimeout 设置连接空闲超时,0 表示不限。
func WithH2IdleTimeout(timeout time.Duration) H2ServerOption {
	return func(h *H2Server) {
		h.idleTimeout = timeout
	}
}

// NewH2Server 创建一个基于 x/net/http2 公开 Framer 的服务端传输,处理 hrpc 请求。
//
// 与 HttpServer 不同,H2Server 不依赖 net/http 与 x/net http2.Server:
//   - 每连接唯一写协程,仿 gRPC loopyWriter 批量合并所有流的响应帧后一次 flush,
//     消除 x/net http2.Server 逐帧立即 flush 的 syscall 瓶颈(实测并发差距主因);
//   - 读协程用 ReadMetaHeaders 合并 HEADERS/CONTINUATION,维护双向流控;
//   - 每请求一个 goroutine 执行 hrpc.Server.Response,响应缓冲后由写协程批量写出。
//
// 仅支持 h2c(prior knowledge,客户端直发前言)模式。
func NewH2Server(pathPrefix string, server *Server, options ...H2ServerOption) *H2Server {
	if pathPrefix != "/" {
		pathPrefix = "/" + strings.Trim(pathPrefix, "/") + "/"
	}
	ret := &H2Server{
		srv:                  server,
		pathPrefix:           pathPrefix,
		maxConcurrentStreams: defaultMaxConcurrentStreams,
		idleTimeout:          defaultServerIdleTimeout,
		done:                 make(chan struct{}),
		conns:                make(map[*h2ServerConn]struct{}),
	}
	for _, option := range options {
		option(ret)
	}
	return ret
}

// H2Server 基于 Framer 的 h2 服务端传输。
type H2Server struct {
	srv                  *Server
	pathPrefix           string
	maxConcurrentStreams uint32
	idleTimeout          time.Duration

	mu     sync.Mutex
	conns  map[*h2ServerConn]struct{}
	closed bool
	done   chan struct{}
}

// Serve 在监听器上循环接受连接,每个连接独立读写协程。
func (h *H2Server) Serve(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-h.done:
				return nil
			default:
				return err
			}
		}
		// 每连接一个 goroutine,避免前一个连接未关闭时阻塞 Accept
		go h.serveConn(conn)
	}
}

// ServeConn 处理单条连接(测试与集成用)。阻塞直到连接关闭。
func (h *H2Server) ServeConn(conn net.Conn) {
	h.serveConn(conn)
}

// serveConn 启动单条连接的处理。
func (h *H2Server) serveConn(nc net.Conn) {
	cc := newH2ServerConn(h, nc)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = nc.Close()
		return
	}
	h.conns[cc] = struct{}{}
	h.mu.Unlock()

	if err := cc.start(); err != nil {
		cc.shutdown(err)
	}
	cc.wait()
	h.mu.Lock()
	delete(h.conns, cc)
	h.mu.Unlock()
}

// Shutdown 停止接受连接,优雅关闭所有在途连接并等待服务端请求完成。
func (h *H2Server) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	close(h.done)
	conns := make([]*h2ServerConn, 0, len(h.conns))
	for cc := range h.conns {
		conns = append(conns, cc)
	}
	h.mu.Unlock()

	for _, cc := range conns {
		cc.shutdown(errors.New("hrpc: h2 server shutdown"))
	}
	for _, cc := range conns {
		cc.wait()
	}
	return h.srv.Shutdown(ctx)
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// h2ServerConn 单条服务端 h2 连接。
type h2ServerConn struct {
	hs         *H2Server
	nc         net.Conn
	queue      *writeQueue[*srvWriteItem]
	done       chan struct{}
	readFramer *http2.Framer
	streams    sync.Map
	streamNum  atomic.Uint32

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

// newH2ServerConn 创建连接对象。读侧 Framer 启用 MetaHeadersFrame 合并。
func newH2ServerConn(hs *H2Server, nc net.Conn) *h2ServerConn {
	cc := &h2ServerConn{
		hs:         hs,
		nc:         nc,
		queue:      newWriteQueue[*srvWriteItem](),
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

// start 完成前言与 SETTINGS 交换,启动读写协程。
func (cc *h2ServerConn) start() error {
	// 读客户端前言(prior knowledge 模式,客户端直发)
	preface := make([]byte, len(http2.ClientPreface))
	if _, err := io.ReadFull(cc.nc, preface); err != nil {
		return err
	}
	if string(preface) != http2.ClientPreface {
		return errors.New("hrpc: invalid client preface")
	}
	// 写服务端 SETTINGS:大接收窗口与最大帧,减少分帧与回补
	var buf bytes.Buffer
	buf.WriteString(http2.ClientPreface)
	fr := http2.NewFramer(&buf, nil)
	_ = fr.WriteSettings(
		http2.Setting{ID: http2.SettingMaxConcurrentStreams, Val: cc.hs.maxConcurrentStreams},
		http2.Setting{ID: http2.SettingInitialWindowSize, Val: recvStreamWindow},
		http2.Setting{ID: http2.SettingMaxFrameSize, Val: recvMaxFrameSize},
		http2.Setting{ID: http2.SettingEnablePush, Val: 0},
	)
	prefaceLen := len(http2.ClientPreface)
	if err := writeAll(cc.nc, buf.Bytes()[prefaceLen:]); err != nil {
		_ = cc.nc.Close()
		return err
	}
	go cc.readLoop()
	go cc.writeLoop()
	if cc.hs.idleTimeout > 0 {
		go cc.idleTimer()
	}
	return nil
}

// wait 等待连接关闭(读/写协程退出)。
func (cc *h2ServerConn) wait() {
	<-cc.done
}

// stream 按流 ID 查找流。
func (cc *h2ServerConn) stream(id uint32) (*serverStream, bool) {
	v, ok := cc.streams.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*serverStream), true
}

// addWindowUpdate 合并回补接收窗口(镜像客户端)。
func (cc *h2ServerConn) addWindowUpdate(streamID uint32, incr uint32) {
	cc.winUpdMu.Lock()
	if cc.winUpd == nil {
		cc.winUpd = make(map[uint32]uint32)
	}
	cc.winUpd[streamID] += incr
	cc.winUpdMu.Unlock()
	if cc.winUpdQueued.CompareAndSwap(false, true) {
		cc.queue.push(&srvWriteItem{control: cc.writeWindowUpdates})
	}
}

// writeWindowUpdates 取出合并增量并批量写 WINDOW_UPDATE。仅写协程调用。
func (cc *h2ServerConn) writeWindowUpdates(fr *http2.Framer) {
	cc.winUpdMu.Lock()
	m := cc.winUpd
	cc.winUpd = nil
	cc.winUpdQueued.Store(false)
	cc.winUpdMu.Unlock()
	for id, incr := range m {
		_ = fr.WriteWindowUpdate(id, incr)
	}
}

// resetStream 入队 RST_STREAM 帧,由写循环批量发出。
func (cc *h2ServerConn) resetStream(id uint32, code http2.ErrCode) {
	cc.queue.push(&srvWriteItem{control: func(fr *http2.Framer) {
		_ = fr.WriteRSTStream(id, code)
	}})
}

// shutdown 关闭连接并失败所有在途流。幂等。
func (cc *h2ServerConn) shutdown(err error) {
	cc.closeOnce.Do(func() {
		cc.errMu.Lock()
		cc.err = err
		cc.errMu.Unlock()
		close(cc.done)
		_ = cc.nc.Close()
	})
	cc.failAll(err)
}

// failAll 取消所有在途流的 handler。
func (cc *h2ServerConn) failAll(err error) {
	cc.streams.Range(func(_, v any) bool {
		st := v.(*serverStream)
		st.fail(err)
		return true
	})
}

// connErr 返回连接关闭原因。
func (cc *h2ServerConn) connErr() error {
	cc.errMu.Lock()
	defer cc.errMu.Unlock()
	if cc.err != nil {
		return cc.err
	}
	return errors.New("hrpc: h2 connection closed")
}

// isClosed 判断连接是否已关闭。
func (cc *h2ServerConn) isClosed() bool {
	select {
	case <-cc.done:
		return true
	default:
		return false
	}
}

// idleTimer 空闲超时后关闭连接。
func (cc *h2ServerConn) idleTimer() {
	timeout := cc.hs.idleTimeout
	if timeout <= 0 {
		return
	}
	ticker := time.NewTicker(timeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if time.Since(time.Unix(0, cc.lastActive.Load())) > timeout {
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

// readLoop 读取帧并分发。窗口回补帧入队批量发出(镜像客户端)。
func (cc *h2ServerConn) readLoop() {
	for {
		cc.lastActive.Store(time.Now().UnixNano())
		f, err := cc.readFramer.ReadFrame()
		if err != nil {
			if !cc.isClosed() {
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
				cc.queue.push(&srvWriteItem{control: func(fr *http2.Framer) {
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
			cc.shutdown(fmt.Errorf("hrpc: client sent GOAWAY: %v", f.ErrCode))
			return
		}
	}
}

// handleSettings 处理客户端 SETTINGS:更新对端最大帧与流窗口,回 ACK。
func (cc *h2ServerConn) handleSettings(f *http2.SettingsFrame) {
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
					v.(*serverStream).sendWin.Add(delta)
					return true
				})
				cc.queue.notify()
			}
		}
		return nil
	})
	cc.queue.push(&srvWriteItem{control: func(fr *http2.Framer) {
		_ = fr.WriteSettingsAck()
	}})
}

// handleWindowUpdate 更新响应发送窗口并唤醒写循环。
func (cc *h2ServerConn) handleWindowUpdate(f *http2.WindowUpdateFrame) {
	incr := int32(f.Increment)
	if f.StreamID == 0 {
		cc.connWin.Add(incr)
	} else if st, ok := cc.stream(f.StreamID); ok {
		st.sendWin.Add(incr)
	}
	cc.queue.notify()
}

// handleHeaders 处理新请求流的 HEADERS,启动 handler goroutine。
func (cc *h2ServerConn) handleHeaders(f *http2.MetaHeadersFrame) {
	if cc.streamNum.Load() >= cc.hs.maxConcurrentStreams {
		cc.resetStream(f.StreamID, http2.ErrCodeRefusedStream)
		return
	}
	if _, ok := cc.stream(f.StreamID); ok {
		// 已存在的流,trailers 场景当前不支持,忽略
		return
	}
	method := f.PseudoValue("method")
	if method != http.MethodPost {
		cc.resetStream(f.StreamID, http2.ErrCodeProtocol)
		return
	}
	st := &serverStream{
		id:       f.StreamID,
		cc:       cc,
		method:   method,
		path:     f.PseudoValue("path"),
		header:   http.Header{},
		bodyCh:   make(chan []byte, 16),
		finishCh: make(chan struct{}),
	}
	st.status.Store(http.StatusOK)
	for _, hf := range f.RegularFields() {
		st.header.Add(hf.Name, hf.Value)
	}
	st.sendWin.Store(cc.peerInitialWin.Load())
	cc.streams.Store(st.id, st)
	cc.streamNum.Add(1)
	if f.StreamEnded() {
		close(st.bodyCh)
	}
	go cc.runStream(st)
}

// handleData 处理请求 DATA:投递数据并回补流控窗口。
func (cc *h2ServerConn) handleData(f *http2.DataFrame) {
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
			select {
			case st.bodyCh <- chunk:
			case <-cc.done:
			case <-st.finishCh:
			}
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
			st.endBody()
		}
	}
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
// 请求流

// serverStream 单个请求流。
type serverStream struct {
	id     uint32
	cc     *h2ServerConn
	method string
	path   string
	header http.Header

	bodyCh   chan []byte
	bodyOnce sync.Once
	finishCh chan struct{}

	cancelMu sync.Mutex
	cancel   context.CancelFunc // setCancel/fail 加锁访问,避免与 readLoop 竞态
	aborted  atomic.Bool
	sendWin  atomic.Int32

	// pendingBytes 待发送响应字节数(write 加/写循环减)。
	// 终结条目必须在流所有数据写完后才能发 END_STREAM,
	// 否则数据仍被流控阻塞时客户端会提前收到流结束。
	pendingBytes atomic.Int64

	mu            sync.Mutex
	committed     bool         // 是否已提交过写条目(决定首次写是否需要 HEADERS)
	status        atomic.Int32 // 响应状态码,默认 200
	headerWritten bool         // HEADERS 是否已写入(仅写循环访问)
	finished      atomic.Bool
	finishOnce    sync.Once
}

// setCancel 登记流的取消函数。若登记前流已被失败,立即触发取消。
func (st *serverStream) setCancel(c context.CancelFunc) {
	st.cancelMu.Lock()
	st.cancel = c
	st.cancelMu.Unlock()
	if st.aborted.Load() {
		c()
	}
}

// fail 取消流的 handler 上下文。
func (st *serverStream) fail(err error) {
	st.cancelMu.Lock()
	c := st.cancel
	st.cancelMu.Unlock()
	if c != nil {
		c()
	}
}

// finish 结束流:从连接移除并关闭 body 通道。
func (st *serverStream) finish() {
	st.finishOnce.Do(func() {
		st.finished.Store(true)
		st.endBody()
		st.cc.streams.Delete(st.id)
		st.cc.streamNum.Add(^uint32(0))
		close(st.finishCh)
	})
}

// endBody 标记请求体结束。
func (st *serverStream) endBody() {
	st.bodyOnce.Do(func() { close(st.bodyCh) })
}

// write 缓冲 handler 的响应写:拷贝数据并作为一批帧提交写循环。
// 首次写附带 HEADERS 提交标记,由写循环按序先写响应头。
func (st *serverStream) write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	st.mu.Lock()
	if st.aborted.Load() {
		st.mu.Unlock()
		return 0, st.cc.connErr()
	}
	first := !st.committed
	st.committed = true
	data := append([]byte(nil), p...)
	st.pendingBytes.Add(int64(len(p)))
	st.mu.Unlock()
	st.cc.queue.push(&srvWriteItem{st: st, data: data, first: first})
	return len(p), nil
}

// complete handler 执行完毕,提交终结帧。
// err 非空且未写任何字节时,以错误状态码终结(与 HttpServer 一致)。
func (st *serverStream) complete(err error) {
	st.mu.Lock()
	if st.aborted.Load() {
		st.mu.Unlock()
		st.finish()
		return
	}
	first := !st.committed
	st.committed = true
	if err != nil && first {
		status, _ := StatusFromError(err)
		st.status.Store(int32(status))
	}
	st.mu.Unlock()
	st.cc.queue.push(&srvWriteItem{st: st, first: first, endStream: true})
}

// runStream 在独立 goroutine 中执行 hrpc 处理链。
func (cc *h2ServerConn) runStream(st *serverStream) {
	ctx, cancel := context.WithCancel(context.Background())
	st.setCancel(cancel)
	defer cancel()

	path := strings.TrimPrefix(st.path, cc.hs.pathPrefix)
	reader := &streamReader{st: st}
	writer := &streamWriter{st: st}
	err := cc.hs.srv.Response(ctx, path, writer, reader, st.header)
	st.complete(err)
}

// streamReader 请求体读取器:从流的数据块通道读。
type streamReader struct {
	st  *serverStream
	cur []byte
	err error
}

func (r *streamReader) Read(p []byte) (int, error) {
	st := r.st
	for {
		if len(r.cur) > 0 {
			n := copy(p, r.cur)
			r.cur = r.cur[n:]
			return n, nil
		}
		if r.err != nil {
			return 0, r.err
		}
		select {
		case chunk, ok := <-st.bodyCh:
			if !ok {
				r.err = io.EOF
				return 0, io.EOF
			}
			r.cur = chunk
		case <-st.cc.done:
			r.err = st.cc.connErr()
			return 0, r.err
		case <-st.finishCh:
			r.err = st.cc.connErr()
			return 0, r.err
		}
	}
}

// streamWriter 响应写入器:缓冲到流,由写循环批量写出。
type streamWriter struct {
	st *serverStream
}

func (w *streamWriter) Write(p []byte) (int, error) {
	return w.st.write(p)
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
// 写协程(仿 gRPC loopyWriter)

// srvWriteItem 写队列条目:响应流帧或控制帧闭包。
type srvWriteItem struct {
	st        *serverStream
	data      []byte // 响应数据(拷贝副本,handler 返回后仍有效)
	dataOff   int    // 数据已写偏移(流控分批跨队列轮次)
	first     bool   // 是否需先写响应 HEADERS
	endStream bool   // 是否携带 END_STREAM
	control   func(fr *http2.Framer)
}

// writeLoop 唯一写协程:批量合并所有流的响应帧后一次 flush 到 TCP。
// flush 时机与客户端对称:队列耗尽且吸收窗口内无新条目,
// 或缓冲达到 maxBatchBytes 才强制 flush。
func (cc *h2ServerConn) writeLoop() {
	var wbuf bytes.Buffer
	fr := http2.NewFramer(&wbuf, nil)
	var leftovers []*srvWriteItem
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
				// 队列空时先短暂自旋,保持写循环热运行
				if cc.queue.spinWait(defaultSpinDuration, cc.done) {
					continue
				}
				select {
				case <-cc.queue.wake:
				case <-cc.done:
					return
				}
				continue
			}
			// 队列暂时耗尽:进入吸收窗口,把窗口期内新到达的条目并入本批。
			// 仅单流时无合并需求,跳过吸收直接 flush 保证串行延迟。
			if hasStream {
				if cc.streamNum.Load() > 1 {
					if cc.absorb(&streamWindow, true) {
						continue
					}
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
			streamWindow, controlWindow = 0, 0 // flush 后窗口归零
			hasStream = false
			continue
		}
		hasStream = false
		for i, item := range batch {
			if item.control != nil {
				item.control(fr)
				continue
			}
			hasStream = true
			st := item.st
			if st.aborted.Load() {
				if item.endStream {
					st.finish()
				}
				continue
			}
			if item.first && !st.headerWritten {
				// 无数据的终结帧由 HEADERS 直接携带 END_STREAM,省一帧
				cc.writeHeaderBlock(fr, st, item.endStream && len(item.data) == 0)
				st.headerWritten = true
				if item.endStream && len(item.data) == 0 {
					st.finish()
					continue
				}
			}
			if cc.writeData(fr, st, item) {
				if item.endStream {
					st.finish()
				}
			} else {
				// 窗口不足,延后等待 WINDOW_UPDATE
				leftovers = append(leftovers, item)
			}
			if wbuf.Len() >= defaultMaxBatchBytes {
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
			// 存在流控阻塞条目:flush 已合并帧,等待 WINDOW_UPDATE 唤醒后继续
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
// 与客户端同款 Nagle 式窗口:命中翻倍、空等减半,流帧/控制帧独立。
func (cc *h2ServerConn) absorb(window *time.Duration, hasStream bool) bool {
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

// writeHeaderBlock 写入响应 HEADERS 帧(仅 :status)。
// endStream 为真时 HEADERS 直接携带 END_STREAM(无响应体场景)。
func (cc *h2ServerConn) writeHeaderBlock(fr *http2.Framer, st *serverStream, endStream bool) {
	var buf bytes.Buffer
	enc := hpack.NewEncoder(&buf)
	enc.WriteField(hpack.HeaderField{Name: ":status", Value: strconv.Itoa(int(st.status.Load()))})
	_ = fr.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      st.id,
		BlockFragment: buf.Bytes(),
		EndStream:     endStream,
		EndHeaders:    true,
	})
}

// writeData 写入响应的 DATA 帧,返回是否有数据因流控未写完。
func (cc *h2ServerConn) writeData(fr *http2.Framer, st *serverStream, item *srvWriteItem) bool {
	if len(item.data) == 0 {
		if item.endStream {
			if st.pendingBytes.Load() > 0 {
				return false // 数据仍被流控阻塞,终结帧延后到数据写完
			}
			// HEADERS 未携带 END_STREAM,补空 DATA 帧终结流
			_ = fr.WriteData(st.id, true, nil)
		}
		return true
	}
	maxFrame := int32(cc.peerMaxFrame.Load())
	data := item.data
	for item.dataOff < len(data) {
		n := int32(len(data) - item.dataOff)
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
			return false // 窗口不足,等待 WINDOW_UPDATE
		}
		cc.connWin.Add(-n)
		st.sendWin.Add(-n)
		st.pendingBytes.Add(-int64(n))
		end := item.dataOff + int(n)
		_ = fr.WriteData(st.id, item.endStream && end == len(data), data[item.dataOff:end])
		item.dataOff = end
	}
	return true
}
