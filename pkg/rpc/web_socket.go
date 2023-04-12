package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"io"
	ht "net/http"
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

type DataRpc struct {
	Type   rpcType   `json:"type"`
	Header ht.Header `json:"header"`
	Data   Raw       `json:"data"`
	Id     int64     `json:"id"`
	Path   string    `json:"path"`
	Status int       `json:"status"`
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type WebSocketRpc struct {
	wsConn   *websocket.Conn
	invoke   Invoke
	id       int64
	response map[int64]chan *DataRpc
	lock     sync.RWMutex
}

type responseWrite struct {
	wsConn *websocket.Conn
	id     int64
	Status int64
}

func (r *responseWrite) Write(p []byte) (n int, err error) {
	data := DataRpc{
		Type:   Response,
		Id:     r.id,
		Status: ht.StatusOK,
		Data:   p,
	}
	buffer, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	err = r.wsConn.WriteMessage(websocket.BinaryMessage, buffer)
	if err != nil {
		return 0, err
	}
	return len(buffer), nil
}

func (r *responseWrite) WriteStatus(status int) error {
	data := DataRpc{
		Type:   Response,
		Id:     r.id,
		Status: status,
	}
	buffer, err := json.Marshal(data)
	if err != nil {
		return err
	}
	err = r.wsConn.WriteMessage(websocket.BinaryMessage, buffer)
	if err != nil {
		return err
	}
	return nil
}

func NewWebSocketRpc(wsConn *websocket.Conn, invoke Invoke) *WebSocketRpc {
	return &WebSocketRpc{
		wsConn:   wsConn,
		invoke:   invoke,
		id:       time.Now().UnixMilli(),
		response: map[int64]chan *DataRpc{},
	}
}

func (w *WebSocketRpc) Run() {
	go func() {
		for {
			_, buffer, err := w.wsConn.ReadMessage()
			if err != nil {
				return
			}
			var data *DataRpc
			err = json.Unmarshal(buffer, &data)
			if err != nil {
				return
			}
			if data.Type == Request {
				func(data *DataRpc) {
					ctx := NewContext(context.TODO())
					defer CloseContext(ctx)
					for key, _ := range data.Header {
						SetHeader(ctx, key, data.Header.Get(key))
					}
					response := &responseWrite{
						wsConn: w.wsConn,
						id:     data.Id,
					}
					err := w.invoke.Invoke(ctx, data.Path, bytes.NewBuffer(data.Data), response)
					if err != nil {
						if res, ok := err.(*Result); ok {
							marshal, err := json.Marshal(res)
							if err != nil {
								return
							}
							_, _ = response.Write(marshal)
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
	data := DataRpc{
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
	for key, val := range GetHeaders(ctx) {
		data.Header.Add(key, val.(string))
	}
	buffer, err = json.Marshal(&data)
	if err != nil {
		return err
	}

	response := make(chan *DataRpc, 1)
	w.lock.Lock()
	w.response[data.Id] = response
	w.lock.Unlock()

	defer func() {
		w.lock.Lock()
		delete(w.response, data.Id)
		w.lock.Unlock()
		//close(response)
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

func NewClientWebSocket(base string) *ClientWebSocket {
	dial, _, err := websocket.DefaultDialer.Dial(base, nil)
	if err != nil {
		return nil
	}

	ret := &ClientWebSocket{
		base:   base,
		client: &ht.Client{},
		rpc:    NewWebSocketRpc(dial, nil),
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

	NewWebSocketRpc(wsConn, s.invoke).Run()
}
