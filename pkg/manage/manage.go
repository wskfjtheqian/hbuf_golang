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

	wsServer   *http.Server
	httpServer *http.Server
	findCancel context.CancelFunc
	httpListen *http.Server
	wssListen  *http.Server
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
		if nil != list.List {
			item := list.List[rand.Intn(len(list.List))]
			if nil != item {
				return item.getClient()
			}
		}
	}
	return nil
}

func (m *Manage) SetConfig(config *Config) {
	m.lock.Lock()
	defer m.lock.Unlock()
	if nil == config {
		if nil != m.httpServer {
		}
		m.config = nil
	}

	if nil != m.config && m.config.Yaml() == config.Yaml() {
		return
	}
	m.config = config

	m.server = map[string]rpc.ServerRouter{}
	if nil != m.httpListen {
		_ = m.httpListen.Close()
		m.httpListen = nil
	}
	if nil != m.wssListen {
		_ = m.wssListen.Close()
		m.wssListen = nil
	}
	if nil != config.Server {
		serConfig := m.config.Server

		m.httpListen = m.startServer(serConfig.Http, func(path string, invoke rpc.Invoke) (http.Handler, string) {
			return rpc.NewServerHttp(path, invoke), "http rpc 服务"
		})
		m.wssListen = m.startServer(serConfig.WebSocket, func(path string, invoke rpc.Invoke) (http.Handler, string) {
			return rpc.NewServerWebSocket(invoke), "web_socket rpc 服务"
		})

		for name, router := range m.install {
			if m.checkOpen(name) {
				if nil != m.httpListen {
					scheme := "http"
					if nil != serConfig.Http.Key && nil != serConfig.Http.Crt {
						scheme = "https"
					}
					m.registerServer(router.router, serConfig.Http, scheme)
				}
				if nil != m.wssListen {
					scheme := "ws"
					if nil != serConfig.WebSocket.Key && nil != serConfig.Http.Crt {
						scheme = "wss"
					}
					m.registerServer(router.router, serConfig.WebSocket, scheme)
				}
				m.rpcServer.Add(router.router)
				m.server[name] = router.router
			}
		}
	}

	if nil != m.findCancel {
		m.findCancel()
		m.findCancel = nil
	}
	m.router = map[string]*clientList{}
	if nil != config.Client {
		cliConfig := config.Client
		if cliConfig.Find {
			go m.findServer()
		}

		if nil != cliConfig.Server {
			for key, list := range cliConfig.Server {
				for _, item := range list {
					m.clientList(key, item, true)
				}
			}
		}
	}
}

//开始远程服务
func (m *Manage) startServer(config *Http, handle func(path string, invoke rpc.Invoke) (http.Handler, string)) *http.Server {
	if nil != config {
		mux := http.NewServeMux()
		path := "/"
		if nil != config.Path {
			path = *config.Path
		}
		h, msg := handle(path[1:], rpc.NewServerJson(m.rpcServer))
		mux.Handle(path, h)

		ser := http.Server{
			Addr:    *config.Address,
			Handler: mux,
		}
		go func() {
			if nil != config.Crt && nil != config.Key {
				log.Println("开启 TLS 加密" + msg + ",addr=" + *config.Address)
				err := ser.ListenAndServeTLS(*config.Crt, *config.Key)
				if err != nil {
					log.Println("开启 TLS 加密" + msg + "失败：" + err.Error())
					return
				}
			} else {
				log.Println("开启 " + msg + ",addr=" + *config.Address)
				err := ser.ListenAndServe()
				if err != nil {
					log.Println("开启" + msg + "失败：" + err.Error())
					return
				}
			}
		}()
		return &ser
	}
	return nil
}

func (m *Manage) Init(ctx context.Context) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if nil != m.config.Server.List {
		for _, item := range *m.config.Server.List {
			if server, ok := m.server[item]; ok {
				log.Println("开启并初始化rpc服务：" + server.GetName())
				server.GetServer().Init(ctx)
			}
		}
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

	name := "/register/server/" + scheme + "/" + *config.Hostname + "/" + router.GetName()
	port := *config.Address
	port = port[strings.Index(port, ":"):]
	value := scheme + "://" + *config.Hostname + port

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
	ctx, cancel := context.WithCancel(context.TODO())
	m.findCancel = cancel
	reps, err := m.etcd.GetClient().Get(ctx, "/register/server/", clientv3.WithPrefix())
	if err != nil {
		log.Println("自动获得服务出错", err.Error())
		return
	}
	for _, item := range reps.Kvs {
		m.editClientList(string(item.Key), string(item.Value), true)
	}

	rch := m.etcd.GetClient().Watch(ctx, "/register/server/", clientv3.WithPrefix())
	for wResp := range rch {
		for _, ev := range wResp.Events {
			log.Println(ev.Kv.Key, ev.Type.String(), ev.Kv.Value)
			if clientv3.EventTypeDelete == ev.Type {
				m.editClientList(string(ev.Kv.Key), string(ev.Kv.Value), false)
			} else {
				m.editClientList(string(ev.Kv.Key), string(ev.Kv.Value), true)
			}
		}
	}
}

//添加或删除服务
func (m *Manage) editClientList(key string, value string, isAdd bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	name := key[17:]
	index := strings.Index(name, "/")
	index = strings.LastIndex(name, "/")
	name = name[index+1:]
	key = key + "/" + value
	m.clientList(name, value, isAdd)
}

func (m *Manage) clientList(name string, address string, isAdd bool) {
	key := address + "/" + name
	if isAdd {
		if val, ok := m.install[name]; ok && nil != val.client {
			var rc rcpClient
			if "local" == address {
				rc = newLocalRpcClient(val.router)
			} else if 0 == strings.Index(address, "https://") || 0 == strings.Index(address, "http://") {
				rc = newHttpRpcClient(address, val.client)
			} else if 0 == strings.Index(address, "ws://") || 0 == strings.Index(address, "wss://") {
				rc = newSocketRpcClient(address, val.client)
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

func newLocalRpcClient(router rpc.ServerRouter) rcpClient {
	return &localRpcClient{
		client: router.GetServer(),
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
