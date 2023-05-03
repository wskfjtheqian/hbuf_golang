package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/nats-io/nats.go"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"io"
	ht "net/http"
	"time"
)

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type MqData struct {
	Header ht.Header `json:"header"`
	Data   Raw       `json:"data"`
	Status int       `json:"status"`
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ClientMq struct {
	client *nats.Conn
}

func NewClientMq(client *nats.Conn) *ClientMq {
	return &ClientMq{
		client: client,
	}
}

func (h *ClientMq) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer) error {
	data := &MqData{}
	for key, val := range GetHeaders(ctx) {
		data.Header.Add(key, val.(string))
	}
	buffer, err := json.Marshal(data)
	if err != nil {
		return erro.Wrap(err)
	}
	response, err := h.client.Request(name, buffer, 100*time.Microsecond)
	if err != nil {
		return erro.Wrap(err)
	}
	err = h.client.Flush()
	if err != nil {
		return erro.Wrap(err)
	}
	err = json.Unmarshal(response.Data, data)
	if err != nil {
		return erro.Wrap(err)
	}
	if data.Status != ht.StatusOK {
		return errors.New(ht.StatusText(data.Status))
	}
	_, err = io.Copy(out, bytes.NewBuffer(data.Data))
	if err != nil {
		return erro.Wrap(err)
	}
	return nil
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ServerMq struct {
	client *nats.Conn
	invoke Invoke
}

func NewServerMq(client *nats.Conn, router ServerRouter) *ServerMq {
	ret := &ServerMq{
		client: client,
	}
	for key, _ := range router.GetInvoke() {
		func(path string) {
			_, err := client.Subscribe(path, func(msg *nats.Msg) {
				err := ret.Subscribe(path, msg)
				if err != nil {
					erro.PrintStack(err)
					return
				}
			})
			if err != nil {
				erro.PrintStack(erro.Wrap(err))
				return
			}
		}("/" + router.GetName() + key)
	}
	return ret
}

func (s *ServerMq) Subscribe(name string, msg *nats.Msg) error {
	ctx := NewContext(context.TODO())
	defer CloseContext(ctx)

	data := &MqData{}
	err := json.Unmarshal(msg.Data, data)
	if err != nil {
		return erro.Wrap(err)
	}
	out := &bytes.Buffer{}
	err = s.invoke.Invoke(ctx, name, bytes.NewBuffer(data.Data), out)

	data = &MqData{}
	if err != nil {
		if res, ok := err.(*Result); ok {
			marshal, err := json.Marshal(res)
			if err != nil {
				return erro.Wrap(err)
			}
			data.Status = ht.StatusOK
			data.Data = marshal
		} else {
			erro.PrintStack(err)
			data.Status = ht.StatusInternalServerError
		}
	} else {
		data.Status = ht.StatusOK
		data.Data = out.Bytes()
	}

	buffer, err := json.Marshal(data)
	if err != nil {
		return erro.Wrap(err)
	}
	err = s.client.Publish(msg.Reply, buffer)
	if err != nil {
		return erro.Wrap(err)
	}
	return nil
}
