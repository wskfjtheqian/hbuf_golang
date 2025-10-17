package hrpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"github.com/gobwas/ws"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const WebSocketConnectId = "WebSocketConnectId"

var now atomic.Pointer[time.Time]

func init() {
	t := time.Now()
	now.Store(&t)
	ticker := time.NewTicker(time.Second)
	go func() {
		for t = range ticker.C {
			now.Store(&t)
		}
	}()
}

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

//func (d data) MarshalJSON() ([]byte, error) {
//	return d, nil
//}
//
//func (d *data) UnmarshalJSON(b []byte) error {
//	*d = b
//	return nil
//}

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

const (
	writeTypeData writeType = iota
	writeTypePong
)

type writeData struct {
	Type writeType
	Data *WebSocketData
}

// ////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func newWebSocket(ctx context.Context, conn net.Conn, response Response) *webSocket {
	ret := &webSocket{
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
	}
	write := make(chan *writeData)
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
	write       atomic.Pointer[chan *writeData]

	requestMiddleware  RequestMiddleware
	response           Response
	responseMiddleware ResponseMiddleware
	ctx                context.Context

	pingInterval time.Duration
	pongWait     time.Duration
	isSendPing   bool
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
			err := s.conn.SetReadDeadline(now.Load().Add(s.pongWait))
			if err != nil {
				herror.PrintStack(err)
				break
			}
			frame, err := ws.ReadFrame(s.conn)
			if err != nil {
				herror.PrintStack(err)
				break
			}

			switch frame.Header.OpCode {
			case ws.OpContinuation:
				println("continuation")
			case ws.OpPing:
				*s.write.Load() <- &writeData{
					Type: writeTypePong,
					Data: nil,
				}
			case ws.OpText:

			case ws.OpBinary:
				var data WebSocketData
				err = s.decoder(bytes.NewBuffer(frame.Payload))(&data, "")
				if err != nil {
					herror.PrintStack(err)
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
			case ws.OpClose:
				break
			}
		}

		write := s.write.Load()
		s.write.Store(nil)

		close(*write)
		_ = s.conn.Close()
		if ticker != nil {
			ticker.Stop()
		}
	}()

	go func() {
		for {
			write := s.write.Load()
			if write == nil {
				break
			}
			data := <-*write
			if data == nil {
				break
			}
			if data.Type == writeTypeData {
				buf := bytes.NewBuffer(nil)
				err := s.encoder(buf)(data.Data, "")
				if err != nil {
					herror.PrintStack(err)
				}

				err = ws.WriteFrame(s.conn, ws.NewBinaryFrame(buf.Bytes()))
				if err != nil {
					herror.PrintStack(err)
					return
				}
			} else if data.Type == writeTypePong {
				err := ws.WriteFrame(s.conn, ws.NewPingFrame(nil))
				if err != nil {
					herror.PrintStack(err)
					return
				}
			}
		}
	}()

	if ticker != nil {
		go func() {
			for _ = range ticker.C {
				err := ws.WriteFrame(s.conn, ws.NewPingFrame(nil))
				if err != nil {
					herror.PrintStack(err)
					return
				}
			}
		}()
	}
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
			*write <- &writeData{
				Type: writeTypeData,
				Data: data,
			}
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
		*write <- &writeData{
			Type: writeTypeData,
			Data: data,
		}

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
		*write <- &writeData{
			Type: writeTypeData,
			Data: response,
		}
		return
	}

	ctx := s.Context()
	for key, values := range data.Header {
		for _, value := range values {
			AddHeader(ctx, key, value)
		}
	}

	if notification {
		err := s.responseMiddleware(func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
			return s.response(ctx, path, writer, reader)
		})(ctx, data.Path, response, data)
		if err != nil {
			herror.PrintStack(err)
		}
		return
	}

	err := s.responseMiddleware(func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
		return s.response(ctx, path, writer, reader)
	})(ctx, strings.TrimLeft(data.Path, "/"), response, data)
	if err != nil {
		err = s.encoder(response)(&Result[hbuf.Data]{
			Code: http.StatusInternalServerError,
			Msg:  "Server error",
		}, "")
		if err != nil {
			herror.PrintStack(err)
			return
		}
	} else {
		response.Status = http.StatusOK
	}
	write := s.write.Load()
	if write == nil {
		return
	}
	*write <- &writeData{
		Type: writeTypeData,
		Data: response,
	}
	return
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

	c.socket = newWebSocket(ctx, conn, c.response)
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
	}

	for _, option := range options {
		option(ret)
	}
	return ret
}

// WebSocketServer WebSocket服务器
type WebSocketServer struct {
	requestMiddleware  RequestMiddleware
	responseMiddleware ResponseMiddleware
	socket             *webSocket
	response           Response
	decode             Decoder
	encode             Encoder
	pingInterval       time.Duration
	pongWait           time.Duration
	isSendPing         bool
}

// Serve 启动WebSocket服务器
func (w *WebSocketServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(request, writer)
	if err != nil {
		http.Error(writer, http.StatusText(http.StatusUpgradeRequired), http.StatusUpgradeRequired)
		return
	}

	w.handleConnection(request.Context(), conn)
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

		w.handleConnection(ctx, conn)
	}
}

// handleConnection 处理WebSocket连接
func (w *WebSocketServer) handleConnection(ctx context.Context, conn net.Conn) {
	w.socket = newWebSocket(ctx, conn, w.response)
	if w.responseMiddleware != nil {
		w.socket.responseMiddleware = w.responseMiddleware
	}
	if w.requestMiddleware != nil {
		w.socket.requestMiddleware = w.requestMiddleware
	}
	if w.decode != nil {
		w.socket.decoder = w.decode
	}
	if w.encode != nil {
		w.socket.encoder = w.encode
	}
	w.socket.isSendPing = w.isSendPing
	w.socket.pongWait = w.pongWait
	w.socket.pingInterval = w.pingInterval
	w.socket.run()
}
