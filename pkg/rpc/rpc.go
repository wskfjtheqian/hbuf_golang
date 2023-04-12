package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"io"
	"reflect"
	"sync"
)

type Context struct {
	context.Context
	done    chan struct{}
	header  map[string]any
	tags    map[string]any
	method  string
	onClone func(ctx context.Context) (context.Context, error)
}

func NewContext(ctx context.Context) context.Context {
	return &Context{
		Context: ctx,
		done:    make(chan struct{}),
		header:  make(map[string]any, 0),
		tags:    make(map[string]any, 0),
	}
}

func (c *Context) Done() <-chan struct{} {
	return c.done
}

func (c *Context) Value(key any) any {
	if reflect.TypeOf(c) == key {
		return c
	}
	return c.Context.Value(key)
}

var contextType = reflect.TypeOf(&Context{})

func SetContextOnClone(ctx context.Context, onClone func(ctx context.Context) (context.Context, error)) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return
	}
	ret.(*Context).onClone = onClone
}

func CloneContext(ctx context.Context) (context.Context, error) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return nil, errors.New("Clone context error, not found hbuf context")
	}

	c := ret.(*Context)
	header := make(map[string]any, 0)
	for key, val := range c.header {
		header[key] = val
	}
	tags := make(map[string]any, 0)
	for key, val := range c.tags {
		tags[key] = val
	}

	c = &Context{
		Context: c.Context,
		done:    make(chan struct{}),
		header:  header,
		tags:    tags,
		onClone: c.onClone,
	}

	if nil == c.onClone {
		return c, nil
	}
	return c.onClone(c)
}

func CloseContext(ctx context.Context) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return
	}
	close(ret.(*Context).done)
}

func SetHeader(ctx context.Context, key string, value any) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return
	}
	ret.(*Context).header[key] = value
}

func GetHeader(ctx context.Context, key string) (value any, ok bool) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return nil, false
	}
	value, ok = ret.(*Context).header[key]
	return
}

func GetHeaders(ctx context.Context) (value map[string]any) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return map[string]any{}
	}
	return ret.(*Context).header
}

func SetTag(ctx context.Context, key string, value any) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return
	}
	ret.(*Context).tags[key] = value
}

func GetTag(ctx context.Context, key string) (value any, ok bool) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return nil, false
	}
	value, ok = ret.(*Context).tags[key]
	return
}

func SetMethod(ctx context.Context, method string) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return
	}
	ret.(*Context).method = method
}

func GetMethod(ctx context.Context) (method string) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return ""
	}
	return ret.(*Context).method
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ServerClient interface {
	GetName() string

	GetId() uint32
}

type Init interface {
	Init()
}

type ServerRouter interface {
	GetName() string

	GetId() uint32

	GetInvoke() map[string]*ServerInvoke

	GetServer() Init
}

type GetServer interface {
	Get(router ServerClient) any
}

type Filter = func(ctx context.Context) (context.Context, error)

type Server struct {
	router map[string]*ServerInvoke
	lock   sync.RWMutex
	filter []Filter
}

func NewServer() *Server {
	ret := Server{
		router: map[string]*ServerInvoke{},
		filter: []Filter{},
	}
	return &ret
}

func (s *Server) Router() map[string]*ServerInvoke {
	return s.router
}

func (s *Server) AddFilter(inc Filter) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.filter = append(s.filter, inc)
}

func (s *Server) InsertFilter(inc Filter) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.filter = append([]Filter{inc}, s.filter...)
}

func (s *Server) Filter(ctx context.Context) (context.Context, error) {
	var err error
	for _, filter := range s.filter {
		ctx, err = filter(ctx)
		if err != nil {
			return nil, err
		}
	}
	return ctx, nil
}

func (s *Server) Add(router ServerRouter) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for key, value := range router.GetInvoke() {
		s.router["/"+router.GetName()+"/"+key] = value
	}
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type Raw []byte

func (m Raw) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return m, nil
}

func (m *Raw) UnmarshalJSON(data []byte) error {
	if m == nil {
		return errors.New("json.RawMessage: UnmarshalJSON on nil pointer")
	}
	*m = append((*m)[0:0], data...)
	return nil
}

type Result struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data Raw    `json:"data"`
}

func (r *Result) SetData(data any) error {
	marshal, err := json.Marshal(data)
	if err != nil {
		return err
	}
	r.Data = marshal
	return nil
}

func (r *Result) GetData(data any) error {
	err := json.Unmarshal(r.Data, data)
	if err != nil {
		return err
	}
	return nil
}

func (r *Result) Error() string {
	return r.Msg
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type Client interface {
	Invoke(ctx context.Context, param hbuf.Data, name string, nameInvoke *ClientInvoke, id int64, idInvoke *ClientInvoke) (hbuf.Data, error)
}

type Invoke interface {
	Invoke(ctx context.Context, name string, in io.Reader, out io.Writer) error
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type ClientInvoke struct {
	ToData func(buf []byte) (hbuf.Data, error)

	FormData func(data hbuf.Data) ([]byte, error)
}

type ServerInvoke struct {
	ToData func(buf []byte) (hbuf.Data, error)

	FormData func(data hbuf.Data) ([]byte, error)

	Invoke func(cxt context.Context, data hbuf.Data) (hbuf.Data, error)

	SetInfo func(cxt context.Context)
}
