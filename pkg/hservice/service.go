package hservice

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	rand2 "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hetcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

type Router struct {
	hrpc.Init
	IsLocal bool
}

type Dispatch func(list []Router, index int32) (hrpc.Init, int32)

type ContextOption func(c *Context)

func WithContextService(service *Service) ContextOption {
	return func(c *Context) {
		c.service = service
	}
}
func WithContextDispatch(dispatch Dispatch) ContextOption {
	return func(c *Context) {
		c.dispatch = dispatch
	}
}

// WithContext 创建一个新的Context
func WithContext(ctx context.Context, options ...ContextOption) context.Context {
	ret := &Context{
		Context: ctx,
	}

	val := ctx.Value(contextType)
	if val != nil {
		ret.service = val.(*Context).service
		ret.dispatch = val.(*Context).dispatch
	}
	for _, option := range options {
		option(ret)
	}
	return ret
}

// Context 是用于处理RPC请求的上下文
type Context struct {
	context.Context
	service  *Service
	dispatch Dispatch
}

var contextType = reflect.TypeOf(&Context{})

// Value 返回Context的value
func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

// FromContext 从Context中获取Context
func FromContext(ctx context.Context) *Service {
	val := ctx.Value(contextType)
	if val == nil {
		return nil
	}
	return val.(*Context).service
}

// ProtocolName 协议名称
const ProtocolName = "hbuf-rpc://"

// RegisterInfo 定义了服务注册信息
type RegisterInfo struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
	Path string `json:"path"`
}

type client struct {
	list     []Router
	dispatch Dispatch
	index    atomic.Int32
}

type Option func(*Service)

func WithServerOption(options ...hrpc.ServerOption) Option {
	return func(s *Service) {
		for _, option := range options {
			option(s.rpcServer)
		}
	}
}

func WithClientOption(options ...hrpc.ClientOption) Option {
	return func(s *Service) {
		s.clientOption = options
	}
}

// NewService 创建一个新的Service实例
func NewService(etcd *hetcd.Etcd, options ...Option) *Service {
	ret := &Service{
		etcd:          etcd,
		install:       make(map[string]*ServerInfo),
		servers:       make(map[string]*ServerInfo),
		clients:       make(map[string]*client),
		httpClient:    make(map[string]*hrpc.Client),
		waitSubscribe: make(chan bool, 2),
	}
	ret.rpcServer = hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewJsonEncode()), hrpc.WithServerDecode(hrpc.NewJsonDecode()))

	for _, option := range options {
		option(ret)
	}
	return ret
}

// Service 定义了一个服务接口
type Service struct {
	config    *Config
	etcd      *hetcd.Etcd
	session   atomic.Pointer[concurrency.Session]
	listen    atomic.Pointer[net.Listener]
	rpcServer *hrpc.Server
	install   map[string]*ServerInfo

	servers    map[string]*ServerInfo
	clients    map[string]*client
	lock       sync.RWMutex
	httpClient map[string]*hrpc.Client

	onRegisterInfo func(info *RegisterInfo)
	onDeleteInfo   func(info *RegisterInfo)

	isSubscribe   bool
	waitSubscribe chan bool
	clientOption  []hrpc.ClientOption
	httpServer    atomic.Pointer[http.Server]
}

// SetConfig 设置配置
func (s *Service) SetConfig(ctx context.Context, cfg *Config) error {
	if s.config.Equal(cfg) {
		return nil
	}
	if cfg == nil {
		err := s.Deregister(ctx)
		if err != nil {
			hlog.Error(ctx, "deregister service failed: %s", err)
		}
		s.config = nil
		return nil
	}

	for key, value := range cfg.Client.Server {
		if install, ok := s.install[key]; ok {
			for _, item := range value {
				if "local" == strings.ToLower(item) {
					s.addLocalClient(install)
				} else {
					parse, err := url.Parse(item)
					if err != nil {
						return err
					}
					err = s.addHttpClient(install, parse)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	s.config = cfg
	err := s.startRpcServer(ctx)
	if err != nil {
		return err
	}

	err = s.Register(ctx)
	if err != nil {
		return err
	}

	if cfg.Client.Find {
		go func() {
			err := s.Discovery(ctx)
			if err != nil {
				hlog.Error(ctx, "discovery service failed: %s", err)
			}
		}()
	}

	for _, item := range cfg.Server.List {
		if install, ok := s.install[item]; ok {
			s.rpcServer.Register(install.id, install.name, install.init, install.methods...)
		}
	}

	defer close(s.waitSubscribe)
	select {
	case <-s.waitSubscribe:
		s.rpcServer.Init(ctx, cfg.Server.List...)
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Register 注册服务到注册中心
func (s *Service) Register(ctx context.Context) error {
	// 检查配置是否为空
	if s.config == nil || s.config.Server == nil {
		return herror.NewError("config is nil or server is nil")
	}
	// 如果配置中未开启注册，则不进行注册
	if s.config == nil || !s.config.Server.Register {
		return nil
	}

	// 获取etcd客户端
	client, err := s.etcd.GetClient()
	if err != nil {
		return err
	}

	leaseTime := s.config.Server.LeaseTime
	if leaseTime == 0 {
		leaseTime = 5
	}
	session, err := concurrency.NewSession(client, concurrency.WithTTL(int(leaseTime)))
	if err != nil {
		return herror.Wrap(err)
	}

	config := s.config.Server.Http
	var path string
	if config != nil && config.Path != nil {
		path = *config.Path
	}
	path = "/" + strings.Trim(path, "/") + "/"

	addr := s.GetServerAddr()
	for _, key := range s.config.Server.List {
		if _, ok := s.install[key]; ok {
			// 构造服务注册信息
			info := &RegisterInfo{
				Name: key,
				Addr: addr,
				Path: path,
			}
			name := ProtocolName + info.Addr + "/" + key
			value, err := json.Marshal(info)
			if err != nil {
				return err
			}

			// 注册服务到etcd
			_, err = client.Put(ctx, name, string(value), clientv3.WithLease(session.Lease()))
			if err != nil {
				return err
			}

			hlog.Info(ctx, "register service success: %s", key)
		}
	}

	s.session.Store(session)
	return nil
}

// Deregister 注销服务从注册中心
func (s *Service) Deregister(ctx context.Context) error {
	// 获取etcd客户端
	client, err := s.etcd.GetClient()
	if err != nil {
		return err
	}

	// 注销服务
	name := ProtocolName + s.GetServerAddr()
	_, err = client.Delete(ctx, name, clientv3.WithPrefix())
	if err != nil {
		return err
	}
	hlog.Info(ctx, "deregister service ")
	defer func() {
		hlog.Info(ctx, "deregister service success")
	}()
	// 释放租约
	session := s.session.Load()
	if session != nil {
		return session.Close()
	}
	return nil
}

// Discovery 发现服务
func (s *Service) Discovery(ctx context.Context) error {
	if s.config == nil || s.config.Server == nil {
		return nil
	}
	if !s.config.Server.Register {
		return nil
	}

	//获取etcd客户端
	client, err := s.etcd.GetClient()
	if err != nil {
		return err
	}

	// 构造服务查询信息
	name := ProtocolName
	resp, err := client.Get(ctx, name, clientv3.WithPrefix())
	if err != nil {
		return err
	}

	// 解析服务信息
	for _, v := range resp.Kvs {
		err := s.parseRegisterInfo(v)
		if err != nil {
			hlog.Error(ctx, "add client failed: %s", err)
		}
	}

	// 监听服务变化
	watchCh := client.Watch(ctx, name, clientv3.WithPrefix())
	for w := range watchCh {
		for _, ev := range w.Events {
			if ev.Type == clientv3.EventTypePut {
				err := s.parseRegisterInfo(ev.Kv)
				if err != nil {
					hlog.Error(ctx, "add client failed: %s", err)
				}
			} else if ev.Type == clientv3.EventTypeDelete {
				err := s.parseDeleteInfo(ev.Kv)
				if err != nil {
					hlog.Error(ctx, "delete client failed: %s", err)
				}
			}
		}
	}
	return nil
}

// startRpcServer 启动RPC服务
func (s *Service) startRpcServer(ctx context.Context) error {
	if s.config == nil || s.config.Server == nil || s.config.Server.Http == nil {
		return nil
	}

	config := s.config.Server.Http
	var path = "/"
	if config.Path != nil {
		path += strings.Trim(*config.Path, "/") + "/"
	}

	mux := http.NewServeMux()
	mux.Handle(path, hrpc.NewHttpServer(path, s.rpcServer))

	//获得IP地址
	address := ":0"
	if config.Address != nil {
		address = *config.Address
	}
	listen, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	s.listen.Store(&listen)
	hlog.Info(ctx, "start https rpc server: %s", listen.Addr())

	go func() {
		if config.Crt != nil && config.Key != nil && *config.Crt != "" && *config.Key != "" {
			// 开启https服务
			server := &http.Server{
				Handler: mux,
			}
			s.httpServer.Store(server)
			err := server.ServeTLS(listen, *config.Crt, *config.Key)
			if err != nil {
				hlog.Error(ctx, "start https rpc server failed: %s", err)
				return
			}
		} else {
			// 1. 生成私钥
			privateKey, err := s.generatePrivateKey()
			if err != nil {
				hlog.Error(ctx, "generate private key failed: %s", err)
				return
			}

			// 5. 生成自签名证书
			cert, err := s.generateSelfSignedCert(privateKey)
			if err != nil {
				hlog.Error(ctx, "generate self signed cert failed: %s", err)
				return
			}

			server := &http.Server{
				Handler: mux,
				TLSConfig: &tls.Config{
					Certificates: []tls.Certificate{cert},
				},
			}
			s.httpServer.Store(server)
			err = server.ServeTLS(listen, "", "")
			if err != nil {
				hlog.Error(ctx, "start https rpc server failed: %s", err)
				return
			}
		}
	}()
	return nil
}

// 生成 ECDSA 私钥
func (s *Service) generatePrivateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// 自签名证书
func (s *Service) generateSelfSignedCert(privateKey *ecdsa.PrivateKey) (tls.Certificate, error) {
	// 填写自签名证书的信息
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Fitten Tech"},
		},
		NotBefore:             htime.NowTime(),
		NotAfter:              htime.NowTime().AddDate(1, 0, 0),
		SubjectKeyId:          []byte{1, 2, 3, 4, 6},
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  false,
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}

	// 自签名证书
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	// 创建 TLS 证书
	cert := tls.Certificate{
		Certificate: [][]byte{certBytes},
		PrivateKey:  privateKey,
	}

	return cert, nil
}

// GetServerAddr 获取服务地址
func (s *Service) GetServerAddr() string {
	listen := s.listen.Load()
	if listen == nil {
		return ""
	}
	addr, port, err := net.SplitHostPort((*listen).Addr().String())
	if err != nil {
		return ""
	}
	if addr != "::" {
		return addr + ":" + port
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	if len(addrs) == 0 {
		return ""
	}
	for _, item := range addrs {
		ipNet, ok := item.(*net.IPNet)
		if !ok {
			continue
		}

		ip := ipNet.IP.To4()
		if ip == nil || ip.String() == "127.0.0.1" {
			continue
		}

		return ip.String() + ":" + port
	}

	return addrs[0].(*net.IPNet).IP.String() + ":" + port
}

// parseRegisterInfo 解析服务注册信息
func (s *Service) parseRegisterInfo(v *mvccpb.KeyValue) error {
	info := &RegisterInfo{}
	err := json.Unmarshal(v.Value, info)
	if err != nil {
		return herror.Wrap(err)
	}

	install, ok := s.install[info.Name]
	if !ok {
		return nil
	}

	addr, err := url.Parse("https://" + info.Addr + "/" + info.Path) //解析服务地址
	if err != nil {
		return err
	}
	err = s.addHttpClient(install, addr)
	if err != nil {
		return err
	}

	if s.onRegisterInfo != nil {
		s.onRegisterInfo(info)
	}
	return nil
}

// parseDeleteInfo 解析服务删除信息
func (s *Service) parseDeleteInfo(v *mvccpb.KeyValue) error {
	info, err := url.Parse(string(v.Key)) //解析服务地址
	if err != nil {
		return err
	}

	//
	//install, ok := s.install[info.Name]
	//if !ok {
	//	return nil
	//}

	err = s.delHttpClient(nil, info.Host)
	if err != nil {
		return err
	}
	//
	//if s.onDeleteInfo != nil {
	//	s.onDeleteInfo(info)
	//}

	return nil
}

// delHttpClient
func (s *Service) delHttpClient(install *ServerInfo, host string) error {
	s.lock.Lock()
	//s.clients[install.name] = hutl.Filter(s.clients[install.name], func(init hrpc.Init) bool {
	//	return "" != install.name
	//})

	delete(s.httpClient, host)
	s.lock.Unlock()

	return nil
}

// addHttpClient 增加HTTP客户端
func (s *Service) addHttpClient(install *ServerInfo, addr *url.URL) error {
	s.lock.Lock()
	connect, ok := s.httpClient[addr.Host]
	if !ok {
		connect = hrpc.NewClient(
			hrpc.NewHttpClient(addr.String()).Request,
			hrpc.WithClientEncoder(hrpc.NewJsonEncode()),
			hrpc.WithClientDecode(hrpc.NewJsonDecode()),
		)
		for _, option := range s.clientOption {
			option(connect)
		}
		s.httpClient[addr.Host] = connect
	}

	c, ok := s.clients[install.name]
	if !ok {
		c = &client{
			list:     make([]Router, 0),
			dispatch: NewDispatchPriorityLocal(NewDispatchRandom()),
		}
		s.clients[install.name] = c
	}
	c.list = append(c.list, Router{
		Init:    install.client(connect),
		IsLocal: false,
	})

	s.checkSubscribe()
	s.lock.Unlock()
	return nil
}

// addLocalClient 增加本地客户端
func (s *Service) addLocalClient(install *ServerInfo) {
	s.lock.Lock()
	c, ok := s.clients[install.name]
	if !ok {
		c = &client{
			list:     make([]Router, 0),
			dispatch: NewDispatchPriorityLocal(NewDispatchRandom()),
		}
		s.clients[install.name] = c
	}
	c.list = append(c.list, Router{
		Init:    install.init,
		IsLocal: true,
	})
	s.checkSubscribe()
	s.lock.Unlock()
}

func (s *Service) NewMiddleware() hrpc.Middleware {
	return func(next hrpc.Handler) hrpc.Handler {
		return func(ctx context.Context, req any) (any, error) {
			return next(WithContext(ctx, WithContextService(s)), req)
		}
	}
}

// GetClient 获取客户端
func (s *Service) GetClient(name string, dispatch Dispatch) hrpc.Init {
	s.lock.Lock()
	defer s.lock.Unlock()

	clients, ok := s.clients[name]
	if !ok || len(clients.list) == 0 {
		return nil
	}
	if dispatch == nil {
		dispatch = clients.dispatch
	}

	c, index := dispatch(clients.list, clients.index.Load())
	clients.index.Add(index)
	return c
}

// 检测服务是否未完成订阅
func (s *Service) checkSubscribe() {
	if !s.isSubscribe {
		temp := true
		for key, _ := range s.install {
			if _, ok := s.clients[key]; !ok {
				temp = false
				break
			}
		}
		if temp {
			s.isSubscribe = true
			s.waitSubscribe <- true
		}
	}
}

func (s *Service) Shutdown(ctx context.Context) error {
	server := s.httpServer.Swap(nil)
	if server == nil {
		return nil
	}

	hlog.Info(ctx, " hrpc server closing")
	defer func() {
		hlog.Info(ctx, "hrpc server closed")
	}()
	return server.Shutdown(ctx)
}

// ServerInfo 服务描述
type ServerInfo struct {
	s       *Service
	methods []*hrpc.Method
	name    string
	id      int32
	client  func(c *hrpc.Client) hrpc.Init
	init    hrpc.Init
}

func (r *ServerInfo) Register(id int32, name string, init hrpc.Init, methods ...*hrpc.Method) {
	r.id = id
	r.name = name
	r.methods = methods
	r.s.install[name] = r
}

func Register[T hrpc.Init](s *Service, init T, server func(r hrpc.ServerRegister, s T), client func(c *hrpc.Client) T) {
	server(&ServerInfo{s: s, init: init, client: func(c *hrpc.Client) hrpc.Init {
		return client(c)
	}}, init)
}

func GetClient(ctx context.Context, name string) hrpc.Init {
	val := ctx.Value(contextType)
	if val == nil {
		return nil
	}

	c := val.(*Context)
	return c.service.GetClient(name, c.dispatch)
}

// NewDispatchRandom 随机调度
func NewDispatchRandom() Dispatch {
	return func(list []Router, index int32) (hrpc.Init, int32) {
		index = rand2.Int32N(int32(len(list)))
		return list[index].Init, index
	}
}

// NewDispatchRoundRobin 轮询调度
func NewDispatchRoundRobin() Dispatch {
	return func(list []Router, index int32) (hrpc.Init, int32) {
		index++
		if index >= int32(len(list)) {
			index = 0
		}
		return list[index].Init, index
	}
}

// NewDispatchHash 哈希调度
func NewDispatchHash(hash uint64) Dispatch {
	return func(list []Router, index int32) (hrpc.Init, int32) {
		index = int32(hash % uint64(len(list)))
		return list[index].Init, index
	}
}

// NewDispatchPriorityLocal 优先本地路由
func NewDispatchPriorityLocal(dispatch Dispatch) Dispatch {
	return func(list []Router, index int32) (hrpc.Init, int32) {
		for _, router := range list {
			if router.IsLocal {
				return router.Init, index
			}
		}
		return dispatch(list, index)
	}
}
