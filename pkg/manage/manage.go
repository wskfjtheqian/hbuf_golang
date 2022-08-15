package manage

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"reflect"
	"sync"
)

type manageContext struct {
	context.Context
	manage *Manage
}

func (d *manageContext) Value(key interface{}) interface{} {
	if reflect.TypeOf(d) == key {
		return d.manage
	}
	return d.Context.Value(key)
}

func (d *manageContext) Done() <-chan struct{} {
	return d.Context.Done()
}

var manageType = reflect.TypeOf(&manageContext{})

func GET(ctx context.Context) *Manage {
	var ret = ctx.Value(manageType)
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
	if nil == ctx.Value(manageType) {
		ctx = &manageContext{
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
