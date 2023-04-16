package manage

import (
	"context"
	etc "github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	clientv3 "go.etcd.io/etcd/client/v3"
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
	config  *Config
	maps    map[string]any
	install map[string]rpc.ServerRouter //安装的服务
	router  map[string]rpc.ServerRouter
	server  map[string]rpc.ServerRouter
	lock    sync.RWMutex
	etcd    *etc.Etcd
}

func NewManage() *Manage {
	ret := &Manage{
		maps:    map[string]any{},
		server:  map[string]rpc.ServerRouter{},
		router:  map[string]rpc.ServerRouter{},
		install: map[string]rpc.ServerRouter{},
	}
	return ret
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
	m.install[r.GetName()] = r
}

func (m *Manage) Get(router rpc.ServerClient) any {
	m.lock.RLock()
	defer m.lock.RUnlock()

	client := m.config.Client
	list := client.List[router.GetName()]
	if nil == list {
		return nil
	}
	for _, item := range list {
		if 0 == len(item) {
			continue
		}
		server, ok := client.Server[item]
		if !ok {
			continue
		}
		if server.Local {
			return m.router[router.GetName()]
		}
	}
	return nil
}

func (m *Manage) SetConfig(config *Config) {
	m.lock.Lock()
	defer m.lock.Unlock()
	if nil == config {
		m.config = nil
	}

	if nil != m.config && m.config.Yaml() == config.Yaml() {
		return
	}
	m.config = config

	for name, router := range m.install {
		if m.checkOpen(name) {
			m.registerServer(router)

			m.router[name] = router
			m.server[name] = router
		}
	}

	if config.Client.Find {
		go m.findServer()
	}
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
		return rpc.NewServerWebSocket(invoke), "web_socket rpc 服务"
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

func (m *Manage) SetEtcd(etcd *etc.Etcd) {
	m.etcd = etcd
}

//注册服务到发现中心
func (m *Manage) registerServer(router rpc.ServerRouter) {
	if !m.config.Server.Register {
		return
	}
	grant, err := m.etcd.GetClient().Grant(context.TODO(), 5)
	if err != nil {
		log.Println("申请租约失败", err.Error())
		return
	}

	name := "/register/server/" + router.GetName()
	value := "127.0.0.1"
	_, err = m.etcd.GetClient().Put(context.TODO(), name, value, clientv3.WithLease(grant.ID))
	if err != nil {
		log.Println("注册服务失败：name=" + name + "; value=" + value)
	} else {
		log.Println("注册服务成功：name=" + name + "; value=" + value)
	}
	_, err = m.etcd.GetClient().KeepAlive(context.TODO(), grant.ID)
	if err != nil {
		log.Println("开始续租失败", err.Error())
		return
	}
}

//处理发现服务
func (m *Manage) findServer() {
	reps, err := m.etcd.GetClient().Get(context.TODO(), "/register/server/", clientv3.WithPrefix())
	if err != nil {
		log.Println("自动获得服务出错", err.Error())
		return
	}
	for _, item := range reps.Kvs {
		m.editServerList(item.Key, item.Value)
	}
	rch := m.etcd.GetClient().Watch(context.TODO(), "/register/server/", clientv3.WithPrefix())
	for wresp := range rch {
		for _, ev := range wresp.Events {
			log.Println(ev.Kv.Key, ev.Type.String(), ev.Kv.Value)
			if clientv3.EventTypeDelete == ev.Type {
				m.editServerList(ev.Kv.Key, nil)
			} else {
				m.editServerList(ev.Kv.Key, ev.Kv.Value)
			}
		}
	}
}

//添加或删除服务
func (m *Manage) editServerList(key []byte, value []byte) {

}
