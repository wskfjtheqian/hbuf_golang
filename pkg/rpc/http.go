package rpc

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	utl "github.com/wskfjtheqian/hbuf_golang/pkg/utils"
	"golang.org/x/net/http2"
	"io"
	ht "net/http"
	"reflect"
	"strings"
)

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ClientHttp struct {
	base   string
	client *ht.Client
}

func NewClientHttp(base string) *ClientHttp {
	transport := &ht.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	if 0 == strings.Index(base, "https://") {
		err := http2.ConfigureTransport(transport)
		if err != nil {
			erro.PrintStack(err)
		}
	}
	return &ClientHttp{
		base: base,
		client: &ht.Client{
			Transport: transport,
		},
	}
}

func (h *ClientHttp) Invoke(ctx context.Context, name string, in io.Reader, out io.Writer, broadcast bool) error {
	request, err := ht.NewRequest("POST", utl.UrlJoin(h.base, name), in)
	if err != nil {
		return err
	}
	for key, values := range GetHeaders(ctx) {
		for _, value := range values {
			request.Header.Add(key, value)
		}
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

// ////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
type httpContext struct {
	context.Context
	value *HttpContextValue
}

type HttpContextValue struct {
	Writer  ht.ResponseWriter
	Request *ht.Request
}

var httpType = reflect.TypeOf(&httpContext{})

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
	ret := ctx.Value(httpType)
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
	ctx := NewContext(context.TODO())
	defer CloseContext(ctx)
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
	err := s.invoke.Invoke(ctx, r.URL.Path[len(s.pathPrefix):], r.Body, w, false)
	if err != nil {
		if res, ok := err.(*Result); ok {
			marshal, err := json.Marshal(res)
			if err != nil {
				return
			}
			_, _ = w.Write(marshal)
			return
		}
		erro.PrintStack(err)
		w.WriteHeader(ht.StatusInternalServerError)
	}
	return
}
