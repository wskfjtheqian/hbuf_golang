package hbuf

import (
	"context"
	"reflect"
	"sync"
)

type Result struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

func (h *Result) Error() string {
	return h.Msg
}

type Context struct {
	context.Context
	header map[string]any
	tags   map[string]any
	method string
}

func NewContext(ctx context.Context) *Context {
	return &Context{
		Context: ctx,
		header:  make(map[string]any, 0),
		tags:    make(map[string]any, 0),
	}
}

func (c *Context) Done() <-chan struct{} {
	return c.Context.Done()
}

func (c *Context) Value(key any) any {
	if reflect.TypeOf(c) == key {
		return c
	}
	return c.Context.Value(key)
}

var contextType = reflect.TypeOf(&Context{})

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

type ServerInvoke struct {
	ToData func(buf []byte) (Data, error)

	FormData func(data Data) ([]byte, error)

	Invoke func(cxt context.Context, data Data) (Data, error)

	SetInfo func(cxt context.Context)
}

type ServerClient interface {
	GetName() string

	GetId() uint32
}

type ServerRouter interface {
	GetName() string

	GetId() uint32

	GetInvoke() map[string]*ServerInvoke
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
