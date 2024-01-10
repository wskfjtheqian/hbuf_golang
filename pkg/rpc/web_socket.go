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
	"strconv"
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
	connectId int64
	header    ht.Header
}

func (w *WebSocketRpc) WsConn() *websocket.Conn {
	return w.wsConn
}

func (w *WebSocketRpc) ConnectId() int64 {
	return w.connectId
}

func (w *WebSocketRpc) SetConnectId(connectId int64) {
	w.connectId = connectId
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

func newWebSocketRpc(wsConn *websocket.Conn, invoke Invoke, header ht.Header) *WebSocketRpc {
	return &WebSocketRpc{
		wsConn:    wsConn,
		invoke:    invoke,
		id:        0,
		connectId: time.Now().UnixMilli(),
		response:  map[int64]chan *WebSocketData{},
		header:    header,
	}
}

func (w *WebSocketRpc) Run() {
	go func() {
		for {
			_, buffer, err := w.wsConn.ReadMessage()
			if err != nil {
				return
			}
			var data *WebSocketData
			err = json.Unmarshal(buffer, &data)
			if err != nil {
				return
			}
			if data.Type == Request {
				go func(data *WebSocketData) {
					response := &webSocketWrite{
						wsConn: w.wsConn,
						id:     data.Id,
					}
					if nil == w.invoke {
						err = response.WriteStatus(ht.StatusNotFound)
						if err != nil {
							erro.PrintStack(err)
							return
						}
						return
					}
					ctx := NewContext(&webSocketContext{
						Context: context.TODO(),
						value:   w,
					})
					defer CloseContext(ctx)

					SetHeader(ctx, WebSocketConnectId, strconv.FormatInt(w.connectId, 10))
					for key, values := range w.header {
						for _, value := range values {
							SetHeader(ctx, key, value)
						}
					}
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
							if err != nil {
								erro.PrintStack(err)
								return
							}
							return
						}
						_ = response.WriteStatus(ht.StatusInternalServerError)
					}
					return
				}(data)
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
}

func (w *WebSocketRpc) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer) error {
	data := WebSocketData{
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
	buffer, err = json.Marshal(&data)
	if err != nil {
		return err
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
	err = w.wsConn.WriteMessage(websocket.BinaryMessage, buffer)
	if err != nil {
		return err
	}

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
	invoke Invoke
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

	newWebSocketRpc(wsConn, s.invoke, r.Header).Run()
}
