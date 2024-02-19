package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	utl "github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"io"
	ht "net/http"
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
	err = s.client.Invoke(ctx, name, bytes.NewReader(buffer), b, nil == nameInvoke.ToData)
	if err != nil {
		return nil, err
	}
	if nil == nameInvoke.ToData {
		return nil, nil
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
}

func NewServerJson(server *Server) *ServerJson {
	return &ServerJson{
		server: server,
	}
}

func (s *ServerJson) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer, broadcast bool) error {
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

	SetContextOnClone(ctx, func(ctx context.Context) (context.Context, error) {
		ctx, _, err := s.server.GetFilter().OnNext(ctx, nil, nil)
		if err != nil {
			return nil, err
		}
		return ctx, nil
	})
	ctx, data, err = s.server.GetFilter().OnNext(ctx, data, func(ctx context.Context, data hbuf.Data) (context.Context, hbuf.Data, error) {
		data, err := value.Invoke(ctx, data)
		if err != nil {
			return nil, nil, err
		}
		return ctx, data, nil
	})
	if err != nil {
		return err
	}

	if value.FormData != nil {
		buffer, err = value.FormData(data)
		if err != nil {
			return err
		}
	} else {
		buffer = nil
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
