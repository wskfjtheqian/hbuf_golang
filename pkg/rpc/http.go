package rpc

import (
	"context"
	"encoding/json"
	"errors"
	utl "github.com/wskfjtheqian/hbuf_golang/pkg/utils"
	"io"
	ht "net/http"
	"reflect"
)

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ClientHttp struct {
	base   string
	client *ht.Client
}

func NewClientHttp(base string) *ClientHttp {
	return &ClientHttp{
		base:   base,
		client: &ht.Client{},
	}
}

func (h *ClientHttp) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer) error {
	request, err := ht.NewRequest("POST", utl.UrlJoin(h.base, name), in)
	if err != nil {
		return err
	}
	for key, val := range GetHeaders(ctx) {
		request.Header.Add(key, val.(string))
	}
	response, err := h.client.Do(request)
	if err != nil {
		return err
	}
	if response.StatusCode != ht.StatusOK {
		return errors.New(response.Status)
	}
	_, err = io.Copy(out, response.Body)
	if err != nil {
		return err
	}
	return nil
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
type httpContext struct {
	context.Context
	value *HttpContextValue
}

type HttpContextValue struct {
	Writer  ht.ResponseWriter
	Request *ht.Request
}

var payType = reflect.TypeOf(&httpContext{})

func (d *httpContext) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d.value
	}
	return d.Context.Value(key)
}

func (d *httpContext) Done() <-chan struct{} {
	return d.Context.Done()
}

func GetHttp(ctx context.Context) *HttpContextValue {
	ret := ctx.Value(payType)
	if nil == ret {
		return nil
	}
	return ret.(*HttpContextValue)
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ServerHttp struct {
	pathPrefix string
	invoke     Invoke
}

func NewServerHttp(pathPrefix string, invoke Invoke) *ServerHttp {
	return &ServerHttp{
		pathPrefix: pathPrefix,
		invoke:     invoke,
	}
}

func (s *ServerHttp) ServeHTTP(w ht.ResponseWriter, r *ht.Request) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	ctx = NewContext(ctx)
	for key, _ := range r.Header {
		SetHeader(ctx, key, r.Header.Get(key))
	}
	ctx = &httpContext{
		Context: ctx,
		value: &HttpContextValue{
			Writer:  w,
			Request: r,
		},
	}
	err := s.invoke.Invoke(ctx, r.URL.Path[len(s.pathPrefix):], r.Body, w)
	if err != nil {
		if res, ok := err.(*Result); ok {
			marshal, err := json.Marshal(res)
			if err != nil {
				return
			}
			_, _ = w.Write(marshal)
			return
		}
		w.WriteHeader(500)
	}
	return
}
