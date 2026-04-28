package hrpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

type rawMessage []byte

func (d rawMessage) Read(p []byte) (n int, err error) {
	n = copy(p, d)
	if n < len(p) {
		err = io.EOF
	}
	return
}

func (d *rawMessage) Write(p []byte) (n int, err error) {
	*d = append(*d, p...)
	return len(p), nil
}

func (d rawMessage) MarshalJSON() ([]byte, error) {
	return d, nil
}

func (d *rawMessage) UnmarshalJSON(b []byte) error {
	*d = b
	return nil
}

type WebSocketData struct {
	Type   Type        `json:"type,omitempty"`
	Header http.Header `json:"header,omitempty"`
	Data   rawMessage  `json:"data,omitempty"`
	Id     uint64      `json:"id,omitempty"`
	Path   string      `json:"path,omitempty"`
	Status int32       `json:"status,omitempty"`
}

func (w *WebSocketData) Read(p []byte) (n int, err error) {
	return w.Data.Read(p)
}

func (w *WebSocketData) Write(p []byte) (n int, err error) {
	return w.Data.Write(p)
}

var webSocketData WebSocketData
var webSocketDataDescriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(webSocketData), nil, map[uint16]hbuf.Descriptor{
	1: hbuf.NewInt8Descriptor(unsafe.Offsetof(webSocketData.Type), false),
	2: hbuf.NewMapDescriptor[string, []string](unsafe.Offsetof(webSocketData.Header), hbuf.NewStringDescriptor(0, false), hbuf.NewListDescriptor[string](0, hbuf.NewStringDescriptor(0, false), false), false),
	3: hbuf.NewBytesDescriptor(unsafe.Offsetof(webSocketData.Data), false),
	4: hbuf.NewUint64Descriptor(unsafe.Offsetof(webSocketData.Id), false),
	5: hbuf.NewStringDescriptor(unsafe.Offsetof(webSocketData.Path), false),
	6: hbuf.NewInt32Descriptor(unsafe.Offsetof(webSocketData.Status), false),
})

func (w *WebSocketData) Descriptors() hbuf.Descriptor {
	return webSocketDataDescriptor
}

type writeType int

// ////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func newWebSocket(ctx context.Context, conn net.Conn, response Response, key string) *webSocket {
	ret := &webSocket{
		key:         key,
		conn:        conn,
		encoder:     NewJsonEncode(),
		decoder:     NewJsonDecode(),
		responseMap: make(map[uint64]chan *WebSocketData),
		response:    response,
		responseMiddleware: func(next Response) Response {
			return response
		},
		requestMiddleware: func(next Request) Request {
			return next
		},
		pingInterval: 5 * time.Second,
		pongWait:     10 * time.Second,
		ctx:          ctx,
	}
	write := make(chan *WebSocketData)
	ret.write.Store(&write)
	return ret
}

type webSocket struct {
	id          atomic.Uint64
	conn        net.Conn
	lock        sync.RWMutex
	encoder     Encoder
	decoder     Decoder
	responseMap map[uint64]chan *WebSocketData
	write       atomic.Pointer[chan *WebSocketData]

	requestMiddleware  RequestMiddleware
	response           Response
	responseMiddleware ResponseMiddleware
	ctx                context.Context

	pingInterval time.Duration
	pongWait     time.Duration
	isSendPing   bool
	onClose      func()
	state        ws.State
	key          string
}

func (s *webSocket) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *webSocket) run() {
	var ticker *time.Ticker
	if s.isSendPing {
		ticker = time.NewTicker(s.pingInterval)
	}

	go func() {
		for {
			err := s.conn.SetReadDeadline(htime.NowTime().Add(s.pongWait))
			if err != nil {
				herror.PrintStack(err)
				break
			}

			payload, opCode, err := wsutil.ReadData(s.conn, s.state)
			if err != nil {
				hlog.Warn("read data error:%v", err)
				break
			}
			switch opCode {
			case ws.OpText:
			case ws.OpBinary:
				var data WebSocketData
				err = s.decoder(bytes.NewBuffer(payload))(&data, "")
				if err != nil {
					herror.PrintStack(err)
				}
				if data.Type == TypePing {
					write := s.write.Load()
					if write != nil {
						*write <- &WebSocketData{Type: TypePong}
					}
					continue
				}
				if data.Type == TypeRequest || data.Type == TypeNotification {
					go s.onResponse(&data, data.Type == TypeNotification)
				} else if data.Type == TypeResponse {
					s.lock.RLock()
					response, ok := s.responseMap[data.Id]
					s.lock.RUnlock()
					if ok {
						response <- &data
					}
				}

			}
		}

		write := s.write.Load()
		s.write.Store(nil)

		close(*write)
		_ = s.conn.Close()
		if ticker != nil {
			ticker.Stop()
		}
		if s.onClose != nil {
			s.onClose()
		}
	}()

	go func() {
		for {
			write := s.write.Load()
			if write == nil {
				break
			}

			writeData := func(data *WebSocketData) {

				buf := bytes.NewBuffer(nil)
				err := s.encoder(buf)(data, "")
				if err != nil {
					herror.PrintStack(err)
				}

				frame := ws.NewBinaryFrame(buf.Bytes())
				if s.state == ws.StateClientSide {
					frame = ws.MaskFrameInPlace(frame)
				}
				err = ws.WriteFrame(s.conn, frame)
				if err != nil {
					herror.PrintStack(err)
					return
				}
			}
			if ticker != nil {
				select {
				case <-ticker.C:
					writeData(&WebSocketData{Type: TypePing})
				case data := <-*write:
					if data == nil {
						break
					}
					writeData(data)
				}
			} else {
				data := <-*write
				if data == nil {
					break
				}
				writeData(data)
			}
		}
	}()
}

// Request 发送请求
func (s *webSocket) Request(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
	return s.requestMiddleware(func(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
		data := &WebSocketData{
			Path:   path,
			Header: http.Header{},
		}

		err := callback(data)
		if err != nil {
			return nil, err
		}

		for key, values := range GetHeaders(ctx) {
			for _, value := range values {
				data.Header.Add(key, value)
			}
		}
		if notification {
			data.Type = TypeNotification
			write := s.write.Load()
			if write == nil {
				return nil, errors.New("connection is closed")
			}
			*write <- data
			return nil, nil
		}

		data.Type = TypeRequest
		data.Id = s.id.Add(1)

		response := make(chan *WebSocketData, 1)
		s.lock.Lock()
		s.responseMap[data.Id] = response
		s.lock.Unlock()

		defer func() {
			s.lock.Lock()
			delete(s.responseMap, data.Id)
			s.lock.Unlock()
			close(response)
		}()

		write := s.write.Load()
		if write == nil {
			return nil, errors.New("connection is closed")
		}
		*write <- data

		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil, errors.New("time out")
		case val := <-response:
			if val.Status != http.StatusOK {
				return nil, errors.New(http.StatusText(int(val.Status)))
			}
			return io.NopCloser(val), nil
		}
	})(ctx, path, notification, callback)
}

// onResponse  当从客户端接收到请求时调用
func (s *webSocket) onResponse(data *WebSocketData, notification bool) {
	response := &WebSocketData{
		Id:     data.Id,
		Type:   TypeResponse,
		Status: http.StatusOK,
	}
	if nil == s.response {
		response.Status = http.StatusNotFound
		write := s.write.Load()
		if write == nil {
			return
		}
		*write <- response
		return
	}

	ctx := s.Context()
	if notification {
		err := s.responseMiddleware(func(ctx context.Context, path string, writer io.Writer, reader io.Reader, header http.Header) error {
			return s.response(ctx, path, writer, reader, header)
		})(ctx, data.Path, response, data, data.Header)
		if err != nil {
			herror.PrintStack(err)
		}
		return
	}

	err := s.responseMiddleware(func(ctx context.Context, path string, writer io.Writer, reader io.Reader, header http.Header) error {
		return s.response(ctx, path, writer, reader, header)
	})(ctx, strings.TrimLeft(data.Path, "/"), response, data, data.Header)
	if err != nil {
		var e *Result[hbuf.Data]
		if errors.As(err, &e) && e.Code == -1 {
			err = s.encoder(response)(&Result[hbuf.Data]{
				Code: http.StatusInternalServerError,
				Msg:  "Server error",
			}, "")
			if err != nil {
				herror.PrintStack(err)
				return
			}
		} else {
			herror.PrintStack(err)
		}
	} else {
		response.Status = http.StatusOK
	}
	write := s.write.Load()
	if write == nil {
		return
	}
	*write <- response
	return
}

func (s *webSocket) Close() {
	err := s.conn.Close()
	if err != nil {
		hlog.Error("close websocket error:%v", err)
		return
	}
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// WebSocketClientOptions WebSocket客户端选项
type WebSocketClientOptions func(c *WebSocketClient)

// WithWebSocketClientResponseMiddleware  设置WebSocket客户端响应中间件
func WithWebSocketClientResponseMiddleware(middleware ...ResponseMiddleware) WebSocketClientOptions {
	return func(c *WebSocketClient) {
		c.responseMiddleware = func(next Response) Response {
			for _, m := range middleware {
				next = m(next)
			}
			return next
		}
	}
}

// WithWebSocketClientRequestMiddleware 设置WebSocket客户端请求中间件
func WithWebSocketClientRequestMiddleware(middleware RequestMiddleware) WebSocketClientOptions {
	return func(c *WebSocketClient) {
		c.requestMiddleware = middleware
	}
}

func WithWebSocketClientDecode(decoder Decoder) WebSocketClientOptions {
	return func(c *WebSocketClient) {
		c.decode = decoder
	}
}

func WithWebSocketClientEncode(encoder Encoder) WebSocketClientOptions {
	return func(c *WebSocketClient) {
		c.encode = encoder
	}
}

func WithWebSocketClientPingInterval(interval time.Duration) WebSocketClientOptions {
	return func(c *WebSocketClient) {
		c.pingInterval = interval
	}
}

func WithWebSocketClientPongWait(wait time.Duration) WebSocketClientOptions {
	return func(c *WebSocketClient) {
		c.pongWait = wait
	}
}

func WithWebSocketClientIsSendPing(isSendPing bool) WebSocketClientOptions {
	return func(c *WebSocketClient) {
		c.isSendPing = isSendPing
	}
}

// NewWebSocketClient 创建一个WebSocket客户端
func NewWebSocketClient(base string, response Response, options ...WebSocketClientOptions) *WebSocketClient {
	ret := &WebSocketClient{
		base:     base,
		response: response,
		requestMiddleware: func(next Request) Request {
			return next
		},
		responseMiddleware: func(next Response) Response {
			return next
		},
		isSendPing:   true,
		pongWait:     60 * time.Second,
		pingInterval: 30 * time.Second,
		dialer: &ws.Dialer{
			TLSConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	for _, option := range options {
		option(ret)
	}
	return ret
}

// WebSocketClient WebSocket客户端
type WebSocketClient struct {
	requestMiddleware  RequestMiddleware
	responseMiddleware ResponseMiddleware
	base               string
	socket             *webSocket
	response           Response
	decode             Decoder
	encode             Encoder
	isSendPing         bool
	pongWait           time.Duration
	pingInterval       time.Duration
	dialer             *ws.Dialer
}

// Connect 连接客户端
func (c *WebSocketClient) Connect(ctx context.Context) error {
	conn, _, _, err := c.dialer.Dial(ctx, c.base)
	if err != nil {
		return err
	}

	c.socket = newWebSocket(ctx, conn, c.response, "")
	if c.responseMiddleware != nil {
		c.socket.responseMiddleware = c.responseMiddleware
	}
	if c.requestMiddleware != nil {
		c.socket.requestMiddleware = c.requestMiddleware
	}
	if c.decode != nil {
		c.socket.decoder = c.decode
	}
	if c.encode != nil {
		c.socket.encoder = c.encode
	}
	c.socket.isSendPing = c.isSendPing
	c.socket.pongWait = c.pongWait
	c.socket.pingInterval = c.pingInterval
	c.socket.run()
	return nil
}

func (c *WebSocketClient) Request(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
	path = "/" + path
	return c.socket.Request(ctx, path, notification, callback)
}

// //////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// WebSocketServerOptions WebSocket服务器选项
type WebSocketServerOptions func(s *WebSocketServer)

// WithWebSocketServerResponseMiddleware 设置WebSocket服务器响应中间件
func WithWebSocketServerResponseMiddleware(middleware ...ResponseMiddleware) WebSocketServerOptions {
	return func(s *WebSocketServer) {
		s.responseMiddleware = func(next Response) Response {
			for i := len(middleware) - 1; i >= 0; i-- {
				next = middleware[i](next)
			}
			return next
		}
	}
}

// WithWebSocketServerRequestMiddleware 设置WebSocket服务器请求中间件
func WithWebSocketServerRequestMiddleware(middleware ...RequestMiddleware) WebSocketServerOptions {
	return func(s *WebSocketServer) {
		s.requestMiddleware = func(next Request) Request {
			for i := len(middleware) - 1; i >= 0; i-- {
				next = middleware[i](next)
			}
			return next
		}
	}
}

func WithWebSocketServerDecode(decoder Decoder) WebSocketServerOptions {
	return func(s *WebSocketServer) {
		s.decode = decoder
	}
}

func WithWebSocketServerEncode(encoder Encoder) WebSocketServerOptions {
	return func(s *WebSocketServer) {
		s.encode = encoder
	}
}

func WithWebSocketServerPingInterval(interval time.Duration) WebSocketServerOptions {
	return func(s *WebSocketServer) {
		s.pingInterval = interval
	}
}

func WithWebSocketServerPongWait(wait time.Duration) WebSocketServerOptions {
	return func(s *WebSocketServer) {
		s.pongWait = wait
	}
}

func WithWebSocketServerIsSendPing(isSendPing bool) WebSocketServerOptions {
	return func(s *WebSocketServer) {
		s.isSendPing = isSendPing
	}
}

func WithWebSocketServerProtocol(check func(s string) bool) WebSocketServerOptions {
	return func(s *WebSocketServer) {
		s.upgrader.Protocol = check
	}
}

// NewWebSocketServer 创建一个WebSocket服务器
func NewWebSocketServer(response Response, options ...WebSocketServerOptions) *WebSocketServer {
	ret := &WebSocketServer{
		response: response,
		requestMiddleware: func(next Request) Request {
			return next
		},
		responseMiddleware: func(next Response) Response {
			return next
		},
		isSendPing:   false,
		pongWait:     60 * time.Second,
		pingInterval: 30 * time.Second,
		upgrader:     &ws.HTTPUpgrader{},
		manager:      NewClientManager(0),
	}

	for _, option := range options {
		option(ret)
	}
	return ret
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// WebSocketServer WebSocket服务器
type WebSocketServer struct {
	requestMiddleware  RequestMiddleware
	responseMiddleware ResponseMiddleware
	manager            *ClientManager
	response           Response
	decode             Decoder
	encode             Encoder
	pingInterval       time.Duration
	pongWait           time.Duration
	isSendPing         bool
	upgrader           *ws.HTTPUpgrader
}

// Serve 启动WebSocket服务器
func (w *WebSocketServer) ServeHTTP(writer http.ResponseWriter, request *http.Request, id string, onConnect func(), onClose func()) {
	conn, _, _, err := w.upgrader.Upgrade(request, writer)
	if err != nil {
		http.Error(writer, http.StatusText(http.StatusUpgradeRequired), http.StatusUpgradeRequired)
		return
	}

	w.handleConnection(request.Context(), conn, id, onConnect, onClose)
}

// ListenAndServe 监听WebSocket服务器
func (w *WebSocketServer) ListenAndServe(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	upgrade := ws.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		_, err = upgrade.Upgrade(conn)
		if err != nil {
			return err
		}
		w.handleConnection(ctx, conn, "", nil, nil)
	}
}

// handleConnection 处理WebSocket连接
func (w *WebSocketServer) handleConnection(ctx context.Context, conn net.Conn, id string, onConnect func(), onClose func()) {
	socket := newWebSocket(ctx, conn, w.response, id)
	if w.responseMiddleware != nil {
		socket.responseMiddleware = w.responseMiddleware
	}
	if w.requestMiddleware != nil {
		socket.requestMiddleware = w.requestMiddleware
	}
	if w.decode != nil {
		socket.decoder = w.decode
	}
	if w.encode != nil {
		socket.encoder = w.encode
	}
	socket.isSendPing = w.isSendPing
	socket.pongWait = w.pongWait
	socket.pingInterval = w.pingInterval

	if onConnect != nil {
		onConnect()
	}
	socket.run()

	if len(id) > 0 {
		old, ok := w.manager.Swap(id, socket)
		if ok {
			old.Close()
			if onClose != nil {
				onClose()
			}
		}
	}

	socket.onClose = func() {
		if w.manager.CompareAndDelete(id, socket) && onClose != nil {
			onClose()
		} else if len(id) == 0 && onConnect != nil {
			onConnect()
		}
	}
}

// CloseClient 关闭客户端
func (w *WebSocketServer) CloseClient(id string) {
	client := w.manager.Del(id)
	if client != nil {
		client.Close()
	}
}

var clientContextType = reflect.TypeOf(&ClientContext{})

func WithClientContextKeys(ctx context.Context, keys ...string) *ClientContext {
	return &ClientContext{
		Context: ctx,
		Range: func(manage *ClientManager, fun func(key string, socket *webSocket) bool) {
			for _, key := range keys {
				if socket, ok := manage.Get(key); ok {
					if !fun(key, socket) {
						break
					}
				}
			}
		},
	}
}

func WithClientContextAll(ctx context.Context) *ClientContext {
	return &ClientContext{
		Context: ctx,
		Range: func(manage *ClientManager, fun func(key string, socket *webSocket) bool) {
			manage.Range(fun)
		},
	}
}

// FromClientContext 从Context中获取Context
func FromClientContext(ctx context.Context) *ClientContext {
	val := ctx.Value(clientContextType)
	if val == nil {
		return nil
	}
	return val.(*ClientContext)
}

type ClientContext struct {
	context.Context
	Range func(manage *ClientManager, fun func(key string, ws *webSocket) bool)
}

func (d *ClientContext) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

func (w *WebSocketServer) Request(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
	a := FromClientContext(ctx)
	if a == nil {
		return nil, herror.NewError("client context is nil")
	}
	var read io.ReadCloser
	var err error

	path = "/" + path
	a.Range(w.manager, func(key string, ws *webSocket) bool {
		read, err = ws.Request(ctx, path, notification, callback)
		return err == nil
	})
	return read, err
}

type ClientShard struct {
	sync.RWMutex
	clients map[string]*webSocket
}

func NewClientManager(shardCount int) *ClientManager {
	if shardCount <= 0 {
		shardCount = runtime.NumCPU() * 8
	}

	// 使用 2 的幂次，便于位运算
	if shardCount&(shardCount-1) != 0 {
		// 找到下一个2的幂次
		shardCount = 1 << uint(math.Ceil(math.Log2(float64(shardCount))))
	}

	shards := make([]*ClientShard, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = &ClientShard{
			clients: make(map[string]*webSocket, 1024), // 预分配容量
		}
	}

	return &ClientManager{
		shards: shards,
		num:    shardCount,
		hasher: func(key string) uint32 {
			// 使用更快的哈希算法
			h := uint32(2166136261)
			for i := 0; i < len(key); i++ {
				h = (h ^ uint32(key[i])) * 16777619
			}
			return h
		},
	}
}

type ClientManager struct {
	shards []*ClientShard
	num    int
	hasher func(string) uint32
}

func (m *ClientManager) getShardIndex(key string) int {
	// 因为是2的幂次，可以用位运算替代取模
	return int(m.hasher(key) & uint32(m.num-1))
}

func (m *ClientManager) Get(key string) (*webSocket, bool) {
	index := m.getShardIndex(key)
	shard := m.shards[index]

	shard.RLock()
	defer shard.RUnlock()

	client, ok := shard.clients[key]
	return client, ok
}

func (m *ClientManager) Swap(key string, socket *webSocket) (*webSocket, bool) {
	index := m.getShardIndex(key)
	shard := m.shards[index]

	shard.Lock()
	defer shard.Unlock()
	old, ok := shard.clients[key]
	shard.clients[key] = socket
	return old, ok
}

func (m *ClientManager) Del(key string) *webSocket {
	index := m.getShardIndex(key)
	shard := m.shards[index]

	shard.Lock()
	defer shard.Unlock()
	client := shard.clients[key]
	delete(shard.clients, key)

	return client
}

func (m *ClientManager) CompareAndDelete(key string, socket *webSocket) bool {
	index := m.getShardIndex(key)
	shard := m.shards[index]

	shard.Lock()
	defer shard.Unlock()

	if val, ok := shard.clients[key]; ok && val == socket {
		delete(shard.clients, key)
		return true
	}
	return false
}

func (m *ClientManager) Range(f func(key string, client *webSocket) bool) {
	for i := 0; i < m.num; i++ {
		shard := m.shards[i]

		shard.RLock()
		shouldContinue := true
		for key, client := range shard.clients {
			if !f(key, client) {
				shouldContinue = false
				break
			}
		}
		shard.RUnlock()

		if !shouldContinue {
			return
		}
	}
}
