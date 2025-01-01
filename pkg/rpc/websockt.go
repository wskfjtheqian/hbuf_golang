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
	//return make([]byte, 0), nil
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
	return &webSocket{
		conn:        conn,
		encoder:     json.NewEncoder(conn),
		decoder:     json.NewDecoder(conn),
		responseMap: make(map[uint64]chan *WebSocketData),
		write:       make(chan *WebSocketData, 10),
		response:    response,
		responseFilter: func(ctx context.Context, response Response) Response {
			return response
		},
		requestFilter: func(ctx context.Context, request Request) Request {
			return request
		},
	}
}

type webSocket struct {
	id          atomic.Uint64
	conn        net.Conn
	lock        sync.RWMutex
	encoder     *json.Encoder
	decoder     *json.Decoder
	responseMap map[uint64]chan *WebSocketData
	write       chan *WebSocketData

	requestFilter  RequestFilter
	response       Response
	responseFilter ResponseFilter
	ctx            context.Context
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

func (ws *webSocket) Request(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
	return ws.requestFilter(ctx, func(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
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
		err := ws.responseFilter(ctx, func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
			return ws.response(ctx, path, writer, reader)
		})(ctx, data.Path, response, data)
		if err != nil {
			erro.PrintStack(err)
		}
		return
	}

	err := ws.responseFilter(ctx, func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
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

// NewWebSocketClient 创建一个WebSocket客户端
func NewWebSocketClient(base string, response Response) *WebSocketClient {
	ret := &WebSocketClient{
		base:     base,
		response: response,
	}
	return ret
}

// WebSocketClient WebSocket客户端
type WebSocketClient struct {
	filter   ResponseFilter
	base     string
	rpc      *webSocket
	response Response
}

// Connect 连接客户端
func (c *WebSocketClient) Connect(ctx context.Context) error {
	conn, _, _, err := ws.Dial(ctx, c.base)
	if err != nil {
		return err
	}

	c.rpc = newWebSocket(ctx, conn, c.response)
	c.rpc.run()
	return nil
}

func (c *WebSocketClient) Request(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
	path = "/" + path
	return c.rpc.Request(ctx, path, notification, callback)
}

// //////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// NewWebSocketServer 创建一个WebSocket服务器
func NewWebSocketServer(port uint16, response Response) *WebSocketServer {
	return &WebSocketServer{
		port:     port,
		response: response,
	}
}

// WebSocketServer WebSocket服务器
type WebSocketServer struct {
	socket   *webSocket
	response Response
	port     uint16
}

// Serve 启动WebSocket服务器
func (w *WebSocketServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(request, writer)
	if err != nil {
		http.Error(writer, "Upgrade failed", http.StatusInternalServerError)
		return
	}

	w.socket = newWebSocket(request.Context(), conn, w.response)
	w.socket.run()
}
