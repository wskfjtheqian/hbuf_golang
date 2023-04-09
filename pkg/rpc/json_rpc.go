package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	utl "github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"io"
	ht "net/http"
	"sync"
)

type ClientJson struct {
	client Invoke
}

func NewJsonClient(client Invoke) *ClientJson {
	return &ClientJson{
		client: client,
	}
}

func (s *ClientJson) Invoke(ctx context.Context, param hbuf.Data, name string, nameInvoke *ClientInvoke, id int64, idInvoke *ClientInvoke) (hbuf.Data, error) {
	buffer, err := nameInvoke.FormData(param)
	if err != nil {
		return nil, utl.Wrap(err)
	}
	b := bytes.NewBuffer(nil)
	err = s.client.Invoke(ctx, name, bytes.NewReader(buffer), b)
	if err != nil {
		return nil, err
	}
	var res Result
	err = json.Unmarshal(b.Bytes(), &res)
	if err != nil {
		return nil, utl.Wrap(err)
	}
	if res.Code != 0 {
		return nil, utl.Wrap(&res)
	}
	data, err := nameInvoke.ToData(res.Data)
	if err != nil {
		return nil, utl.Wrap(err)
	}
	return data, nil
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ServerJson struct {
	server *Server
	lock   sync.RWMutex
}

func NewServerJson(server *Server) *ServerJson {
	return &ServerJson{
		server: server,
	}
}

func (s *ServerJson) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer) error {
	value, ok := s.server.Router()[name]
	if !ok {
		return &Result{Code: ht.StatusNotFound, Msg: "not found"}
	}
	buffer, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	data, err := value.ToData(buffer)
	if err != nil {
		return err
	}
	SetMethod(ctx, name)
	value.SetInfo(ctx)
	ctx, err = s.server.Filter(ctx)
	if nil != err {
		return err
	}
	SetContextOnClone(ctx, func(ctx context.Context) (context.Context, error) {
		c, err := s.server.Filter(ctx)
		if err != nil {
			return nil, err
		}
		return c, nil
	})
	data, err = value.Invoke(ctx, data)
	if err != nil {
		return err
	}
	buffer, err = value.FormData(data)
	if err != nil {
		return err
	}
	buffer, err = json.Marshal(Result{
		Code: 0,
		Msg:  "Ok",
		Data: buffer,
	})
	if err != nil {
		return err
	}
	_, err = out.Write(buffer)
	if err != nil {
		return err
	}
	return nil
}
