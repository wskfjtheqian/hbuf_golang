package manage

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"log"
	"net/http"
	"reflect"
	"sync"
)

type Context struct {
	context.Context
	manage *Manage
}

func (d *Context) Value(key any) any {
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
	maps   map[string]any
	router map[string]rpc.ServerRouter
	server map[string]rpc.ServerRouter
	lock   sync.RWMutex
}

func NewManage() *Manage {
	ret := &Manage{
		maps:   map[string]any{},
		server: map[string]rpc.ServerRouter{},
		router: map[string]rpc.ServerRouter{},
	}
	return ret
}

func (m *Manage) SetConfig(config *Config) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if nil != m.config && m.config.Yaml() == config.Yaml() {
		return
	}
	m.config = config

}

func (m *Manage) OnFilter(ctx context.Context, data hbuf.Data, in *rpc.Filter, call rpc.FilterCall) (context.Context, hbuf.Data, error) {
	if nil == ctx.Value(cType) {
		ctx = &Context{
			ctx,
			m,
		}
	}
	return in.OnNext(ctx, data, call)
}

func (m *Manage) Add(r rpc.ServerRouter) {
	m.lock.Lock()
	defer m.lock.Unlock()
	//if m.checkOpen(r.GetName()) {
	//	m.router[r.GetName()] = r
	//	m.server[r.GetName()] = r
	//}
}

func (m *Manage) Get(router rpc.ServerClient) any {
	m.lock.RLock()
	defer m.lock.RUnlock()

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

func (m *Manage) startServer(config *Http, handle func(path string, invoke rpc.Invoke) (http.Handler, string)) {
	if nil != config {
		mux := http.NewServeMux()
		server := rpc.NewServer()
		for _, value := range m.router {
			server.Add(value)
		}
		path := "/"
		if nil != config.Path {
			path = *config.Path
		}
		h, msg := handle(path, rpc.NewServerJson(server))
		mux.Handle(path, h)
		go func() {
			if nil != config.Crt && nil != config.Key {
				log.Println("开启 TLS 加密" + msg + ",addr=" + *config.Address)
				err := http.ListenAndServeTLS(*config.Address, *config.Crt, *config.Key, mux)
				if err != nil {
					log.Println("开启 TLS 加密" + msg + "失败：" + err.Error())
					return
				}
			} else {
				log.Println("开启 " + msg + ",addr=" + *config.Address)
				err := http.ListenAndServe(*config.Address, mux)
				if err != nil {
					log.Println("开启" + msg + "失败：" + err.Error())
					return
				}
			}
		}()
	}
}

func (m *Manage) Init(ctx context.Context) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	m.startServer(m.config.Server.Http, func(path string, invoke rpc.Invoke) (http.Handler, string) {
		return rpc.NewServerHttp(path, invoke), "http rpc 服务"
	})
	m.startServer(m.config.Server.WebSocket, func(path string, invoke rpc.Invoke) (http.Handler, string) {
		return rpc.NewServerWebSocket(invoke), "web_socket rpc  服务"
	})
	for _, server := range m.server {
		log.Println("开启并初始化rpc服务：" + server.GetName())
		server.GetServer().Init(ctx)
	}
}

func (m *Manage) checkOpen(name string) bool {
	if nil == m.config.Server.List {
		return false
	}
	for _, item := range *m.config.Server.List {
		if item == name {
			return true
		}
	}
	return false
}
