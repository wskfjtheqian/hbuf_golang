package manage

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"reflect"
	"sync"
)

type Context struct {
	context.Context
	manage *Manage
}

func (d *Context) Value(key interface{}) interface{} {
	if reflect.TypeOf(d) == key {
		return d.manage
	}
	return d.Context.Value(key)
}

func (d *Context) Done() <-chan struct{} {
	return d.Context.Done()
}

var cType = reflect.TypeOf(&Context{})

func GET(ctx context.Context) *Manage {
	var ret = ctx.Value(cType)
	if nil == ret {
		return nil
	}
	return ret.(*Manage)
}

type Manage struct {
	config *Config
	maps   map[string]interface{}
	router map[string]hbuf.ServerRouter
	lock   sync.RWMutex
}

func NewManage(con *Config) *Manage {
	return &Manage{
		config: con,
		maps:   map[string]interface{}{},
		router: map[string]hbuf.ServerRouter{},
	}
}

func (m *Manage) OnFilter(ctx context.Context) (context.Context, error) {
	if nil == ctx.Value(cType) {
		ctx = &Context{
			ctx,
			m,
		}
	}
	return ctx, nil
}

func (m *Manage) Add(r hbuf.ServerRouter) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.router[r.GetName()] = r
}

func (m *Manage) Get(router hbuf.ServerClient) interface{} {
	client := m.config.Client
	sers := client.List[router.GetName()]
	if nil == sers {
		return nil
	}
	for _, item := range sers {
		if 0 == len(item) {
			continue
		}
		server, ok := client.Server[item]
		if !ok {
			continue
		}
		if nil != server.Local && *server.Local {
			return m.router[router.GetName()]
		}
	}
	return nil
}
