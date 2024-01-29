package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"io"
	ht "net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

var upGrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 65536,
	CheckOrigin: func(r *ht.Request) bool {
		return true
	},
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type rpcType int

const Request = 0
const Response = 1
const WebSocketConnectId = "WebSocketConnectId"

type WebSocketData struct {
	Type   rpcType   `json:"type"`
	Header ht.Header `json:"header"`
	Data   Raw       `json:"data"`
	Id     int64     `json:"id"`
	Path   string    `json:"path"`
	Status int       `json:"status"`
}

func (w *WebSocketData) Write(p []byte) (n int, err error) {
	w.Data = p
	return len(p), err
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type webSocketContext struct {
	context.Context
	value *WebSocketRpc
}

var webSocketType = reflect.TypeOf(&webSocketContext{})

func (d *webSocketContext) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d.value
	}
	return d.Context.Value(key)
}

func (d *webSocketContext) Done() <-chan struct{} {
	return d.Context.Done()
}

func GetWebSocket(ctx context.Context) *WebSocketRpc {
	ret := ctx.Value(webSocketType)
	if nil == ret {
		return nil
	}
	return ret.(*WebSocketRpc)
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type WebSocketRpc struct {
	wsConn   *websocket.Conn
	invoke   Invoke
	id       int64
	response map[int64]chan *WebSocketData
	lock     sync.RWMutex
	Context  func() context.Context
	write    chan *WebSocketData
}

func (w *WebSocketRpc) WsConn() *websocket.Conn {
	return w.wsConn
}

type webSocketWrite struct {
	wsConn *websocket.Conn
	id     int64
	Status int64
}

func (r *webSocketWrite) Write(p []byte) (n int, err error) {
	data := WebSocketData{
		Type:   Response,
		Id:     r.id,
		Status: ht.StatusOK,
		Data:   p,
	}
	buffer, err := json.Marshal(data)
	if err != nil {
		return 0, erro.Wrap(err)
	}
	err = r.wsConn.WriteMessage(websocket.BinaryMessage, buffer)
	if err != nil {
		return 0, erro.Wrap(err)
	}
	return len(buffer), nil
}

func (r *webSocketWrite) WriteStatus(status int) error {
	data := WebSocketData{
		Type:   Response,
		Id:     r.id,
		Status: status,
	}
	buffer, err := json.Marshal(data)
	if err != nil {
		return erro.Wrap(err)
	}
	err = r.wsConn.WriteMessage(websocket.BinaryMessage, buffer)
	if err != nil {
		return erro.Wrap(err)
	}
	return nil
}

func newWebSocketRpc(wsConn *websocket.Conn, invoke Invoke, ctx func() context.Context) *WebSocketRpc {
	return &WebSocketRpc{
		wsConn:   wsConn,
		invoke:   invoke,
		id:       0,
		response: map[int64]chan *WebSocketData{},
		Context:  ctx,
		write:    make(chan *WebSocketData),
	}
}

func (w *WebSocketRpc) Run() {
	go func() {
		for {
			_, buffer, err := w.wsConn.ReadMessage()
			if err != nil {
				erro.PrintStack(err)
				return
			}
			var data *WebSocketData
			err = json.Unmarshal(buffer, &data)
			if err != nil {
				return
			}
			if data.Type == Request {
				go w.onRequest(data)
			} else if data.Type == Response {
				w.lock.RLock()
				response, ok := w.response[data.Id]
				w.lock.RUnlock()
				if ok {
					response <- data
				}
			}
		}
	}()

	go func() {
		for {
			write := <-w.write
			marshal, err := json.Marshal(write)
			if err != nil {
				erro.PrintStack(err)
				return
			}
			err = w.wsConn.WriteMessage(websocket.BinaryMessage, marshal)
			if err != nil {
				erro.PrintStack(err)
				return
			}
		}

	}()
}

func (w *WebSocketRpc) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer) error {
	data := &WebSocketData{
		Type:   Request,
		Path:   "/" + name,
		Id:     atomic.AddInt64(&w.id, 1),
		Header: ht.Header{},
	}
	buffer, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	data.Data = buffer
	for key, values := range GetHeaders(ctx) {
		for _, value := range values {
			data.Header.Add(key, value)
		}
	}

	response := make(chan *WebSocketData, 1)
	w.lock.Lock()
	w.response[data.Id] = response
	w.lock.Unlock()

	defer func() {
		w.lock.Lock()
		delete(w.response, data.Id)
		w.lock.Unlock()
		close(response)
	}()
	w.write <- data

	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		return errors.New("time out")
	case val := <-response:
		if val.Status != ht.StatusOK {
			return errors.New(ht.StatusText(val.Status))
		}
		_, _ = out.Write(val.Data)
	}
	return nil
}

func (w *WebSocketRpc) onRequest(data *WebSocketData) {
	response := &WebSocketData{
		Id:     data.Id,
		Type:   Response,
		Status: ht.StatusOK,
	}
	if nil == w.invoke {
		response.Status = ht.StatusNotFound
		w.write <- response
		return
	}
	var ctx context.Context
	if w.Context == nil || !IsContext(w.Context()) {
		ctx = NewContext(context.TODO())
	} else {
		ctx = w.Context()
	}

	ctx = &webSocketContext{
		Context: ctx,
		value:   w,
	}
	defer CloseContext(ctx)

	for key, _ := range data.Header {
		SetHeader(ctx, key, data.Header.Get(key))
	}

	err := w.invoke.Invoke(ctx, data.Path, bytes.NewBuffer(data.Data), response)
	if err != nil {
		if res, ok := err.(*Result); ok {
			marshal, err := json.Marshal(res)
			if err != nil {
				erro.PrintStack(err)
				return
			}
			_, err = response.Write(marshal)
			w.write <- response
			return
		} else {
			response.Status = ht.StatusInternalServerError
		}
	} else {
		response.Status = ht.StatusOK
	}
	w.write <- response
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ClientWebSocket struct {
	base   string
	client *ht.Client
	rpc    *WebSocketRpc
}

func NewClientWebSocket(base string, invoke Invoke) *ClientWebSocket {
	dial, _, err := websocket.DefaultDialer.Dial(base, nil)
	if err != nil {
		return nil
	}

	ret := &ClientWebSocket{
		base:   base,
		client: &ht.Client{},
		rpc:    newWebSocketRpc(dial, invoke, nil),
	}
	ret.rpc.Run()
	return ret
}

func (h *ClientWebSocket) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer) error {
	return h.rpc.Invoke(ctx, name, in, out)
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ServerWebSocket struct {
	invoke  Invoke
	Context func() context.Context
	rpc     *WebSocketRpc
}

func NewServerWebSocket(invoke Invoke) *ServerWebSocket {
	return &ServerWebSocket{
		invoke: invoke,
	}
}

func (s *ServerWebSocket) ServeHTTP(w ht.ResponseWriter, r *ht.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		return
	}
	wsConn, err := upGrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	s.rpc = newWebSocketRpc(wsConn, s.invoke, s.Context)
	s.rpc.Run()
}

func (h *ServerWebSocket) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer) error {
	return h.rpc.Invoke(ctx, name, in, out)
}
