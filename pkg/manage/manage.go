package manage

import (
	"context"
	etc "github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	clientv3 "go.etcd.io/etcd/client/v3"
	"log"
	"math/rand"
	"net/http"
	"reflect"
	"strings"
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

type CallClient func(client rpc.Client) rpc.Init

type serverClient struct {
	router rpc.ServerRouter
	client CallClient
}

type clientList struct {
	Keys map[string]rcpClient
	List []rcpClient
}

type Manage struct {
	config    *Config
	maps      map[string]any
	install   map[string]*serverClient    //安装的服务
	router    map[string]*clientList      //已获取的服务地址
	server    map[string]rpc.ServerRouter //开启的服务
	lock      sync.RWMutex
	etcd      *etc.Etcd
	rpcServer *rpc.Server
}

func NewManage() *Manage {
	ret := &Manage{
		maps:      map[string]any{},
		server:    map[string]rpc.ServerRouter{},
		router:    map[string]*clientList{},
		install:   map[string]*serverClient{},
		rpcServer: rpc.NewServer(),
	}
	return ret
}

func (m *Manage) RpcServer() *rpc.Server {
	return m.rpcServer
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

func (m *Manage) Add(r rpc.ServerRouter, c CallClient) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.install[r.GetName()] = &serverClient{
		router: r,
		client: c,
	}
}

func (m *Manage) Get(router rpc.ServerClient) any {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if list, ok := m.router[router.GetName()]; ok && 0 < len(list.List) {
		return list.List[rand.Intn(len(list.List))].getClient()
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

	isHttp := m.startServer(m.config.Server.Http, func(path string, invoke rpc.Invoke) (http.Handler, string) {
		return rpc.NewServerHttp(path, invoke), "http rpc 服务"
	})
	isSockt := m.startServer(m.config.Server.WebSocket, func(path string, invoke rpc.Invoke) (http.Handler, string) {
		return rpc.NewServerWebSocket(invoke), "web_socket rpc 服务"
	})

	for name, router := range m.install {
		if m.checkOpen(name) {
			if isHttp {
				scheme := "http"
				if nil != m.config.Server.Http.Key {
					scheme = "https"
				}
				m.registerServer(router.router, m.config.Server.Http, scheme)
			}
			if isSockt {
				scheme := "ws"
				if nil != m.config.Server.WebSocket.Key {
					scheme = "wss"
				}
				m.registerServer(router.router, m.config.Server.WebSocket, scheme)
			}
			m.rpcServer.Add(router.router)
			m.server[name] = router.router
		}
	}

	if config.Client.Find {
		go m.findServer()
	}

}

//开始远程服务
func (m *Manage) startServer(config *Http, handle func(path string, invoke rpc.Invoke) (http.Handler, string)) bool {
	if nil != config {
		mux := http.NewServeMux()
		path := "/"
		if nil != config.Path {
			path = *config.Path
		}
		h, msg := handle(path[1:], rpc.NewServerJson(m.rpcServer))
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
		return true
	}
	return false
}

func (m *Manage) Init(ctx context.Context) {
	m.lock.RLock()
	defer m.lock.RUnlock()

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
func (m *Manage) registerServer(router rpc.ServerRouter, config *Http, scheme string) {
	if !m.config.Server.Register {
		return
	}
	grant, err := m.etcd.GetClient().Grant(context.TODO(), 5)
	if err != nil {
		log.Println("申请租约失败", err.Error())
		return
	}

	name := "/register/server/" + scheme + "/" + router.GetName()
	port := (*config.Address)
	port = port[strings.Index(port, ":"):]
	value := scheme + "://127.0.0.1" + port

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
		m.editCleintList(string(item.Key), string(item.Value), true)
	}
	rch := m.etcd.GetClient().Watch(context.TODO(), "/register/server/", clientv3.WithPrefix())
	for wresp := range rch {
		for _, ev := range wresp.Events {
			log.Println(ev.Kv.Key, ev.Type.String(), ev.Kv.Value)
			if clientv3.EventTypeDelete == ev.Type {
				m.editCleintList(string(ev.Kv.Key), string(ev.Kv.Value), false)
			} else {
				m.editCleintList(string(ev.Kv.Key), string(ev.Kv.Value), true)
			}
		}
	}
}

//添加或删除服务
func (m *Manage) editCleintList(key string, value string, isAdd bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	name := key[17:]
	index := strings.Index(name, "/")
	types := name[:index]
	name = name[index+1:]
	key = key + "/" + value

	if isAdd {
		if val, ok := m.install[name]; ok && nil != val.client {
			var rc rcpClient
			if "http" == types || "https" == types {
				rc = newHttpRpcClient(value, val.client)
			} else if "ws" == types || "wss" == types {
				rc = newSocketRpcClient(value, val.client)
			}
			routers, ok := m.router[name]
			if !ok {
				routers = &clientList{
					Keys: map[string]rcpClient{},
					List: []rcpClient{},
				}
				m.router[name] = routers
			}

			routers.Keys[key] = rc
			list := make([]rcpClient, len(routers.Keys))
			i := 0
			for _, client := range routers.Keys {
				list[i] = client
				i++
			}
			routers.List = list
		}
	} else {
		if routers, ok := m.router[name]; ok {
			delete(routers.Keys, key)
			list := make([]rcpClient, len(routers.Keys))
			i := 0
			for _, client := range routers.Keys {
				list[i] = client
				i++
			}
			routers.List = list
		}
	}
}

type rcpClient interface {
	getClient() rpc.Init
}

type localRpcClient struct {
	client rpc.Init
}

func newLocalRpcClient(router rpc.Init) rcpClient {
	return &localRpcClient{
		client: router,
	}
}

func (c *localRpcClient) getClient() rpc.Init {
	return c.client
}

type httpRpcClient struct {
	client rpc.Init
}

func newHttpRpcClient(url string, call CallClient) rcpClient {
	client := rpc.NewClientHttp(url)
	jsonClient := rpc.NewJsonClient(client)
	return &httpRpcClient{
		client: call(jsonClient),
	}
}

func (c *httpRpcClient) getClient() rpc.Init {
	return c.client
}

type socketRpcClient struct {
	client rpc.Init
}

func newSocketRpcClient(url string, call CallClient) rcpClient {
	client := rpc.NewClientWebSocket(url)
	jsonClient := rpc.NewJsonClient(client)
	return &socketRpcClient{
		client: call(jsonClient),
	}

}

func (c *socketRpcClient) getClient() rpc.Init {
	return c.client
}
