package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"math/big"
	rand2 "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// WithContext 创建一个新的Context
func WithContext(ctx context.Context, service *Service) context.Context {
	return &Context{
		Context: ctx,
		service: service,
	}
}

// Context 是用于处理RPC请求的上下文
type Context struct {
	context.Context
	service *Service
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

// 协议名称
const ProtocolName = "hbuf-rpc://"

// RegisterInfo 定义了服务注册信息
type RegisterInfo struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
	Path string `json:"path"`
}

type Option func(*Service)

func WithMiddleware(middlewares ...rpc.HandlerMiddleware) Option {
	return func(s *Service) {
		s.middlewares = append(s.middlewares, middlewares...)
	}
}

// NewService 创建一个新的Service实例
func NewService(etcd *etcd.Etcd, options ...Option) *Service {
	ret := &Service{
		etcd:       etcd,
		install:    make(map[string]*ServerInfo),
		servers:    make(map[string]*ServerInfo),
		clients:    make(map[string][]Init),
		httpClient: make(map[string]*rpc.Client),
	}

	for _, option := range options {
		option(ret)
	}

	ret.middleware = func(next rpc.Handler) rpc.Handler {
		for i := len(ret.middlewares) - 1; i >= 0; i-- {
			next = ret.middlewares[i](next)
		}
		return next
	}
	ret.rpcServer = rpc.NewServer(rpc.WithServerMiddleware(append(ret.middlewares, ret.NewMiddleware())...), rpc.WithServerEncoder(rpc.NewHBufEncode()), rpc.WithServerDecode(rpc.NewHBufDecode()))

	return ret
}

// Service 定义了一个服务接口
type Service struct {
	config    *Config
	etcd      *etcd.Etcd
	lease     atomic.Pointer[clientv3.LeaseGrantResponse]
	listen    atomic.Pointer[net.Listener]
	rpcServer *rpc.Server
	install   map[string]*ServerInfo

	servers     map[string]*ServerInfo
	clients     map[string][]Init
	lock        sync.RWMutex
	httpClient  map[string]*rpc.Client
	middleware  rpc.HandlerMiddleware
	middlewares []rpc.HandlerMiddleware
}

// SetConfig 设置配置
func (s *Service) SetConfig(cfg *Config) error {
	if s.config.Equal(cfg) {
		return nil
	}
	ctx := context.Background()
	if cfg == nil {
		err := s.Deregister(ctx)
		if err != nil {
			hlog.Error("deregister service failed: %s", err)
		}
		s.config = nil
		return nil
	}

	for _, item := range cfg.Server.List {
		if install, ok := s.install[item]; ok {
			_, _ = s.middleware(func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
				install.init.Init(ctx)
				return nil, nil
			})(ctx, nil)
			s.rpcServer.Register(install.id, install.name, install.methods...)
		}
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
	err := s.startRpcServer()
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
				hlog.Error("discovery service failed: %s", err)
			}
		}()
	}
	return nil
}

// Register 注册服务到注册中心
func (s *Service) Register(ctx context.Context) error {
	// 检查配置是否为空
	if s.config == nil || s.config.Server == nil {
		return erro.NewError("config is nil or server is nil")
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

	lease := s.lease.Load()
	if lease != nil {
		_, err = client.Revoke(context.Background(), lease.ID)
		if err != nil {
			hlog.Error("revoke lease failed: %s", err)
		}
	}

	leaseTime := s.config.Server.LeaseTime
	if leaseTime == 0 {
		leaseTime = 5
	}

	//申请租约
	lease, err = client.Grant(ctx, leaseTime)
	if err != nil {
		return err
	}

	config := s.config.Server.Http
	var path string
	if config != nil && config.Path != nil {
		path = *config.Path
	}
	path = "/" + strings.Trim(path, "/") + "/"

	for key, _ := range s.install {
		// 构造服务注册信息
		info := &RegisterInfo{
			Name: key,
			Addr: s.GetServerAddr(),
			Path: path,
		}
		name := ProtocolName + info.Addr + "/" + key
		value, err := json.Marshal(info)
		if err != nil {
			return err
		}

		// 注册服务到etcd
		_, err = client.Put(ctx, name, string(value), clientv3.WithLease(lease.ID))
		if err != nil {
			return err
		}

		hlog.Info("register service success: %s", key)
	}

	// 保持租约
	alive, err := client.KeepAlive(ctx, lease.ID)
	if err != nil {
		return err
	}
	go func() {
		for range alive {
		}
	}()

	s.lease.Store(lease)
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
	hlog.Info("deregister service success")

	// 释放租约
	lease := s.lease.Load()
	if lease != nil {
		_, err = client.Revoke(ctx, lease.ID)
		if err != nil {
			hlog.Error("revoke lease failed: %s", err)
		}
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
			hlog.Error("add client failed: %s", err)
		}
	}

	// 监听服务变化
	watchCh := client.Watch(ctx, name, clientv3.WithPrefix())
	for w := range watchCh {
		for _, ev := range w.Events {
			if ev.Type == clientv3.EventTypePut {
				err := s.parseRegisterInfo(ev.Kv)
				if err != nil {
					hlog.Error("add client failed: %s", err)
				}
			} else if ev.Type == clientv3.EventTypeDelete {
				hlog.Info("service deregister: %s", string(ev.Kv.Key))
			}
		}
	}
	return nil
}

// startRpcServer 启动RPC服务
func (s *Service) startRpcServer() error {
	if s.config == nil || s.config.Server == nil || s.config.Server.Http == nil {
		return nil
	}

	config := s.config.Server.Http
	var path = "/"
	if config.Path != nil {
		path += strings.Trim(*config.Path, "/") + "/"
	}

	mux := http.NewServeMux()
	mux.Handle(path, rpc.NewHttpServer(path, s.rpcServer))

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
	hlog.Info("start https rpc server: %s", listen.Addr())

	go func() {
		if config.Crt != nil && config.Key != nil && *config.Crt != "" && *config.Key != "" {
			// 开启https服务
			server := &http.Server{
				Handler: mux,
			}
			err := server.ServeTLS(listen, *config.Crt, *config.Key)
			if err != nil {
				hlog.Error("start https rpc server failed: %s", err)
				return
			}
		} else {
			// 1. 生成私钥
			privateKey, err := s.generatePrivateKey()
			if err != nil {
				hlog.Error("generate private key failed: %s", err)
				return
			}

			// 5. 生成自签名证书
			cert, err := s.generateSelfSignedCert(privateKey)
			if err != nil {
				hlog.Error("generate self signed cert failed: %s", err)
				return
			}

			server := &http.Server{
				Handler: mux,
				TLSConfig: &tls.Config{
					Certificates: []tls.Certificate{cert},
				},
			}
			err = server.ServeTLS(listen, "", "")
			if err != nil {
				hlog.Error("start https rpc server failed: %s", err)
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
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0),
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
	_, port, err := net.SplitHostPort((*listen).Addr().String())
	if err != nil {
		return ""
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	if len(addrs) == 0 {
		return ""
	}
	return addrs[0].(*net.IPNet).IP.String() + ":" + port
}

// parseRegisterInfo 解析服务注册信息
func (s *Service) parseRegisterInfo(v *mvccpb.KeyValue) error {
	info := &RegisterInfo{}
	err := json.Unmarshal(v.Value, info)
	if err != nil {
		return erro.Wrap(err)
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
	return nil
}

// addHttpClient 增加HTTP客户端
func (s *Service) addHttpClient(install *ServerInfo, addr *url.URL) error {
	s.lock.Lock()
	connect, ok := s.httpClient[addr.Host]
	if !ok {
		connect = rpc.NewClient(
			rpc.NewHttpClient(addr.String()).Request,
			rpc.WithClientEncoder(rpc.NewHBufEncode()),
			rpc.WithClientDecode(rpc.NewHBufDecode()),
		)
		s.httpClient[addr.Host] = connect
	}
	s.lock.Unlock()
	client := install.client(connect)

	s.lock.Lock()
	s.clients[install.name] = append(s.clients[install.name], client)
	s.lock.Unlock()
	return nil
}

// addLocalClient 增加本地客户端
func (s *Service) addLocalClient(install *ServerInfo) {
	s.lock.Lock()
	s.clients[install.name] = append(s.clients[install.name], install.init)
	s.lock.Unlock()
}

func (s *Service) NewMiddleware() rpc.HandlerMiddleware {
	return func(next rpc.Handler) rpc.Handler {
		return func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
			return next(WithContext(ctx, s), req)
		}
	}
}

// GetClient 获取客户端
func (s *Service) GetClient(name string) Init {
	s.lock.Lock()
	defer s.lock.Unlock()

	clients, ok := s.clients[name]
	length := len(clients)
	if !ok || length == 0 {
		return nil
	}

	return clients[rand2.Int32N(int32(length))]
}

type Init interface {
	Init(ctx context.Context)
}

// 服务描述
type ServerInfo struct {
	s       *Service
	methods []*rpc.Method
	name    string
	id      int32
	client  func(c *rpc.Client) Init
	init    Init
}

func (r *ServerInfo) Register(id int32, name string, methods ...*rpc.Method) {
	r.id = id
	r.name = name
	r.methods = methods
	r.s.install[name] = r
}

func Register[T Init](s *Service, init T, server func(r rpc.ServerRegister, s T), client func(c *rpc.Client) T) {
	server(&ServerInfo{s: s, init: init, client: func(c *rpc.Client) Init {
		return client(c)
	}}, init)
}

func GetClient(ctx context.Context, name string) Init {
	s := FromContext(ctx)
	if s == nil {
		return nil
	}
	return s.GetClient(name)
}
