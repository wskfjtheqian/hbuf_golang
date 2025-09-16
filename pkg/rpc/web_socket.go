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
	"net/url"
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

const WebSocketConnectId = "WebSocketConnectId"

type WebSocketData struct {
	Type   rpcType   `json:"type,omitempty"`
	Header ht.Header `json:"header,omitempty"`
	Data   Raw       `json:"data,omitempty"`
	Id     int64     `json:"id,omitempty"`
	Path   string    `json:"path,omitempty"`
	Status int       `json:"status,omitempty"`
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
	wsConn    *websocket.Conn
	invoke    Invoke
	id        int64
	response  map[int64]chan *WebSocketData
	lock      sync.RWMutex
	Context   func() context.Context
	write     chan *WebSocketData
	closeCall func()
}

func (w *WebSocketRpc) WsConn() *websocket.Conn {
	return w.wsConn
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
				break
			}
			var data *WebSocketData
			err = json.Unmarshal(buffer, &data)
			if err != nil {
				return
			}
			if data.Type == Request || data.Type == Broadcast {
				go w.onRequest(data, data.Type == Broadcast)
			} else if data.Type == Heartbeat {
				w.write <- &WebSocketData{
					Type: Heartbeat,
				}
			} else if data.Type == Response {
				w.lock.RLock()
				response, ok := w.response[data.Id]
				w.lock.RUnlock()
				if ok {
					response <- data
				}
			}
		}
		if w.closeCall != nil {
			w.closeCall()
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

func (w *WebSocketRpc) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer, broadcast bool) error {
	data := &WebSocketData{
		Path:   "/" + name,
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
	if broadcast {
		data.Type = Broadcast
		w.write <- data
		return nil
	}

	data.Type = Request
	data.Id = atomic.AddInt64(&w.id, 1)

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

func (w *WebSocketRpc) onRequest(data *WebSocketData, broadcast bool) {
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

	if broadcast {
		err := w.invoke.Invoke(ctx, data.Path, bytes.NewBuffer(data.Data), response, true)
		if err != nil {
			erro.PrintStack(err)
		}
		return
	}

	err := w.invoke.Invoke(ctx, data.Path, bytes.NewBuffer(data.Data), response, false)
	if err != nil {
		var res *Result
		if !errors.As(err, &res) {
			erro.PrintStack(err)
			res = &Result{
				Code: 1,
				Msg:  "Server error",
			}
		}
		marshal, err := json.Marshal(res)
		if err != nil {
			erro.PrintStack(err)
			return
		}
		_, err = response.Write(marshal)
	} else {
		response.Status = ht.StatusOK
	}
	w.write <- response
}

func (w *WebSocketRpc) Close() error {
	if nil != w.wsConn {
		return w.wsConn.Close()
	}
	return nil
}

func (w *WebSocketRpc) OnClose(f func()) {
	w.closeCall = f
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ClientWebSocket struct {
	base   string
	client *ht.Client
	rpc    *WebSocketRpc
}

func NewClientWebSocket(base string, invoke Invoke, params *url.Values) *ClientWebSocket {
	requestHeader := ht.Header{}
	if nil != params {
		requestHeader.Add("Sec-Websocket-Protocol", url.QueryEscape(params.Encode()))
	}
	dial, _, err := websocket.DefaultDialer.Dial(base, requestHeader)
	if err != nil {
		erro.PrintStack(err)
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

func (h *ClientWebSocket) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer, broadcast bool) error {
	return h.rpc.Invoke(ctx, name, in, out, broadcast)
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ServerWebSocket struct {
	invoke    Invoke
	rpc       *WebSocketRpc
	Context   func() context.Context
	OnAuth    func(request *ht.Request, Query url.Values) bool
	OnConnect func() bool
	OnClose   func()
}

func NewServerWebSocket(invoke Invoke) *ServerWebSocket {
	return &ServerWebSocket{
		invoke: invoke,
	}
}

func (s *ServerWebSocket) ServeHTTP(w ht.ResponseWriter, r *ht.Request) {
	protocol := r.Header.Get("Sec-Websocket-Protocol")
	auth := true
	var responseHeader ht.Header
	if len(protocol) > 0 {
		responseHeader = ht.Header{"Sec-Websocket-Protocol": {protocol}}
		if nil != s.OnAuth {
			unescape, err := url.QueryUnescape(protocol)
			if err == nil {
				query, err := url.ParseQuery(unescape)
				if err == nil {
					auth = s.OnAuth(r, query)
				}
			}
		}
	}
	if !websocket.IsWebSocketUpgrade(r) {
		return
	}
	wsConn, err := upGrader.Upgrade(w, r, responseHeader)
	if err != nil {
		erro.PrintStack(err)
		return
	}

	data := &WebSocketData{}
	if auth {
		data.Type = AuthSuccess
	} else {
		data.Type = AuthFailure
	}
	marshal, err := json.Marshal(data)
	if err != nil {
		erro.PrintStack(err)
		return
	}
	_ = wsConn.WriteMessage(websocket.BinaryMessage, marshal)
	if !auth {
		_ = wsConn.Close()
		return
	}
	s.rpc = newWebSocketRpc(wsConn, s.invoke, s.Context)
	s.rpc.OnClose(s.OnClose)
	s.rpc.Run()

	if s.OnConnect != nil {
		s.OnConnect()
	}
}

func (h *ServerWebSocket) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer, broadcast bool) error {
	return h.rpc.Invoke(ctx, name, in, out, broadcast)
}

func (s *ServerWebSocket) Close() error {
	if nil == s.rpc {
		return nil
	}
	return s.rpc.Close()
}
