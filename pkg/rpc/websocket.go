package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/gobwas/ws"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const WebSocketConnectId = "WebSocketConnectId"

type Data struct {
	bytes.Buffer `json:"-"`
}

// MarshalJSON 返回 m 的 JSON 编码。
func (m Data) MarshalJSON() ([]byte, error) {
	return m.Bytes(), nil
}

// UnmarshalJSON 将 JSON 编码的数据解码到 m 中。
func (m *Data) UnmarshalJSON(data []byte) error {
	if m == nil {
		return errors.New("json.RawMessage: UnmarshalJSON on nil pointer")
	}
	_, err := m.Write(data)
	return err
}

type WebSocketData struct {
	Type   Type        `json:"type,omitempty"`
	Header http.Header `json:"header,omitempty"`
	Data   Data        `json:"data,omitempty"`
	Id     uint64      `json:"id,omitempty"`
	Path   string      `json:"path,omitempty"`
	Status int         `json:"status,omitempty"`
}

func (w *WebSocketData) Write(p []byte) (n int, err error) {
	return w.Data.Write(p)
}

func (w *WebSocketData) Read(p []byte) (n int, err error) {
	return w.Data.Read(p)
}

// ////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func newWebSocket(ctx context.Context, conn net.Conn, response Response) *webSocket {
	ret := &webSocket{
		conn:        conn,
		encoder:     json.NewEncoder(conn),
		decoder:     json.NewDecoder(conn),
		responseMap: make(map[uint64]chan *WebSocketData),
		write:       make(chan *WebSocketData, 10),
		response:    response,
		responseMiddleware: func(next Response) Response {
			return response
		},
		requestMiddleware: func(next Request) Request {
			return next
		},
	}

	return ret
}

type webSocket struct {
	id          atomic.Uint64
	conn        net.Conn
	lock        sync.RWMutex
	encoder     *json.Encoder
	decoder     *json.Decoder
	responseMap map[uint64]chan *WebSocketData
	write       chan *WebSocketData

	requestMiddleware  RequestMiddleware
	response           Response
	responseMiddleware ResponseMiddleware
	ctx                context.Context
}

func (r *webSocket) Context() context.Context {
	if r.ctx != nil {
		return r.ctx
	}
	return context.Background()
}

func (ws *webSocket) run() {
	go func() {
		for {
			var data *WebSocketData
			err := ws.decoder.Decode(&data)
			if err != nil {
				erro.PrintStack(err)
				break
			}
			if data.Type == TypeRequest || data.Type == TypeNotification {
				go ws.onResponse(data, data.Type == TypeNotification)
			} else if data.Type == TypeResponse {
				ws.lock.RLock()
				response, ok := ws.responseMap[data.Id]
				ws.lock.RUnlock()
				if ok {
					response <- data
				}
			}
		}
		close(ws.write)
	}()

	go func() {
		for {
			data := <-ws.write
			err := ws.encoder.Encode(data)
			if err != nil {
				erro.PrintStack(err)
				return
			}
		}
	}()
}

// Request 发送请求
func (ws *webSocket) Request(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
	return ws.requestMiddleware(func(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
		data := &WebSocketData{
			Path:   "/" + path,
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
			ws.write <- data
			return nil, nil
		}

		data.Type = TypeRequest
		data.Id = ws.id.Add(1)

		response := make(chan *WebSocketData, 1)
		ws.lock.RLock()
		ws.responseMap[data.Id] = response
		ws.lock.RUnlock()

		defer func() {
			ws.lock.Lock()
			delete(ws.responseMap, data.Id)
			ws.lock.Unlock()
			close(response)
		}()
		ws.write <- data

		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil, errors.New("time out")
		case val := <-response:
			if val.Status != http.StatusOK {
				return nil, errors.New(http.StatusText(val.Status))
			}
			return io.NopCloser(val), nil
		}
	})(ctx, path, notification, callback)
}

// onResponse  当从客户端接收到请求时调用
func (ws *webSocket) onResponse(data *WebSocketData, notification bool) {
	response := &WebSocketData{
		Id:     data.Id,
		Type:   TypeResponse,
		Status: http.StatusOK,
	}
	if nil == ws.response {
		response.Status = http.StatusNotFound
		ws.write <- response
		return
	}

	ctx := ws.Context()
	for key, values := range data.Header {
		for _, value := range values {
			AddHeader(ctx, key, value)
		}
	}

	if notification {
		err := ws.responseMiddleware(func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
			return ws.response(ctx, path, writer, reader)
		})(ctx, data.Path, response, data)
		if err != nil {
			erro.PrintStack(err)
		}
		return
	}

	err := ws.responseMiddleware(func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
		return ws.response(ctx, path, writer, reader)
	})(ctx, strings.TrimLeft(data.Path, "/"), response, data)
	if err != nil {
		err = json.NewEncoder(response).Encode(&Result[hbuf.Data]{
			Code: http.StatusInternalServerError,
			Msg:  "Server error",
		})
		if err != nil {
			erro.PrintStack(err)
			return
		}
	} else {
		response.Status = http.StatusOK
	}
	ws.write <- response
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
}

// Connect 连接客户端
func (c *WebSocketClient) Connect(ctx context.Context) error {
	conn, _, _, err := ws.Dial(ctx, c.base)
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
}

// Serve 启动WebSocket服务器
func (w *WebSocketServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(request, writer)
	if err != nil {
		http.Error(writer, "Upgrade failed", http.StatusInternalServerError)
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
	w.socket.run()
}
