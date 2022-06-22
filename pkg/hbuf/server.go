package hbuf

import (
	"context"
	"reflect"
	"sync"
)

type Result struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

func (h *Result) Error() string {
	return h.Msg
}

type Context struct {
	context.Context
	header map[string]interface{}
}

func NewContext(ctx context.Context) *Context {
	return &Context{
		ctx,
		make(map[string]interface{}, 0),
	}
}

func (c *Context) Value(key interface{}) interface{} {
	if reflect.TypeOf(c) == key {
		return c.header
	}
	return c.Context.Value(key)
}

var contextType = reflect.TypeOf(&Context{})

func SetHeader(ctx context.Context, key string, value interface{}) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return
	}
	ret.(Context).header[key] = value
}

func GetHeader(ctx context.Context, key string) (value interface{}, ok bool) {
	var ret = ctx.Value(contextType)
	if nil == ret {
		return nil, false
	}
	value, ok = ret.(Context).header[key]
	return
}

type ServerInvoke struct {
	ToData func(buf []byte) (Data, error)

	FormData func(data Data) ([]byte, error)

	Invoke func(cxt context.Context, data Data) (Data, error)
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
	Get(router ServerClient) interface{}
}

type Filter = func(ctx *Context) (*Context, error)

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

func (s *Server) Filter(ctx *Context) (*Context, error) {
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
