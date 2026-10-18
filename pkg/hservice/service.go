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
	"errors"
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

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hetcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

type Router struct {
	hrpc.Init
	isLocal bool
	host    string
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

// http2Proto 服务端 TLS ALPN 协议标识
const http2Proto = "h2"

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
		s.serverOptions = append(s.serverOptions, options...)
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
	// 默认 JSON 编解码，可通过 Option 或 Config.Codec 覆盖
	ret.serverOptions = []hrpc.ServerOption{
		hrpc.WithServerEncoder(hrpc.NewJsonEncode()),
		hrpc.WithServerDecode(hrpc.NewJsonDecode()),
	}

	for _, option := range options {
		option(ret)
	}
	ret.rpcServer = hrpc.NewServer(ret.serverOptions...)
	return ret
}

// Service 定义了一个服务接口
type Service struct {
	config    *Config
	etcd      *hetcd.Etcd
	session   atomic.Pointer[concurrency.Session]
	listen    atomic.Pointer[net.Listener]
	addr      atomic.Pointer[string] // GetServerAddr 解析结果缓存，保证注册与注销使用一致地址
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
	serverOptions []hrpc.ServerOption // 重建 rpcServer 时重放的选项（默认 HBuf 编解码 + 用户自定义）
	httpServer    atomic.Pointer[http.Server]
	h2Server      atomic.Pointer[hrpc.H2Server]

	keepMu     sync.Mutex
	keepCancel context.CancelFunc
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

	// 清理旧配置的资源：注销旧 key、关闭旧 session、停止旧 keepSession。
	// 不清理则旧 key 的租约被旧 session 持续续约，永久残留在注册中心。
	s.keepMu.Lock()
	if s.keepCancel != nil {
		s.keepCancel()
		s.keepCancel = nil
	}
	s.keepMu.Unlock()
	if oldAddr := s.GetServerAddr(); oldAddr != "" {
		if client, err := s.etcd.GetClient(); err == nil {
			_, _ = client.Delete(ctx, ProtocolName+oldAddr, clientv3.WithPrefix())
		}
	}
	if old := s.session.Swap(nil); old != nil {
		_ = old.Close()
	}

	// 提前保存配置：后续 addHttpClient/startRpcServer/rpcServer 重建均依赖新配置
	s.config = cfg

	// 按配置重建 rpcServer：切换编解码器需要重建 Server，已安装服务会在下方重新注册。
	// 配置项 Codec 优先于 NewService 的默认 JSON 与 WithServerOption 自定义项。
	options := make([]hrpc.ServerOption, 0, len(s.serverOptions)+2)
	options = append(options, s.serverOptions...)
	options = append(options,
		hrpc.WithServerEncoder(s.newEncoder()),
		hrpc.WithServerDecode(s.newDecoder()),
	)
	s.rpcServer = hrpc.NewServer(options...)

	// Client 配置可选：为 nil 时跳过静态客户端建立，仅作为服务端或纯发现节点运行
	if cfg.Client != nil {
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
	}

	err := s.startRpcServer(ctx)
	if err != nil {
		return err
	}

	err = s.Register(ctx)
	if err != nil {
		return err
	}

	// 替换旧的 keepSession 守护协程，避免多次 SetConfig 后重复注册
	keepCtx, keepCancel := context.WithCancel(ctx)
	s.keepMu.Lock()
	if s.keepCancel != nil {
		s.keepCancel()
	}
	s.keepCancel = keepCancel
	s.keepMu.Unlock()
	go s.keepSession(keepCtx)

	if cfg.Client != nil && cfg.Client.Find {
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

	// 等待所有已安装服务都获得客户端后再初始化。
	// checkSubscribe 需持锁调用（与 addHttpClient/addLocalClient 保持一致），
	// 提前调用一次可覆盖「无已安装服务」或「客户端已就绪」两种立即满足的场景。
	s.lock.Lock()
	s.checkSubscribe()
	subscribed := s.isSubscribe
	s.lock.Unlock()

	if !subscribed {
		select {
		case <-s.waitSubscribe:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.rpcServer.Init(ctx, cfg.Server.List...)
	return nil
}

// 守护 session
func (s *Service) keepSession(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			session := s.session.Load()
			if session != nil {
				<-session.Done()
			}

			time.Sleep(time.Second * 2)
			err := s.Register(ctx)
			if err != nil {
				hlog.Error(ctx, "register service failed: %s", err)
			}
		}
	}
}

// Register 注册服务到注册中心
func (s *Service) Register(ctx context.Context) error {
	s.session.Store(nil)

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
	// Path 为 "/" 时构造出 "//"，会导致客户端路由 URL 与路径拼接错误
	if path == "//" {
		path = "/"
	}

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

	// RPC 服务未启动时服务地址为空，此时以空前缀删除会误删其他服务的注册信息
	addr := s.GetServerAddr()
	if addr == "" {
		return herror.NewError("server addr is empty, deregister aborted")
	}

	// 注销服务
	name := ProtocolName + addr
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

	// 监听服务变化。从全量拉取的下一个 revision 开始监听，
	// 否则 Get 与 Watch 间隙中发生的删除事件会永久丢失。
	watchCh := client.Watch(ctx, name, clientv3.WithPrefix(), clientv3.WithRev(resp.Header.Revision+1))
	for {
		select {
		case <-ctx.Done():
			hlog.Info(ctx, "discovery watch stopped: context canceled")
			return ctx.Err()
		case w, ok := <-watchCh:
			if !ok {
				hlog.Warn(ctx, "discovery watch channel closed")
				return nil
			}
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
	}
}

// startRpcServer 启动RPC服务
func (s *Service) startRpcServer(ctx context.Context) error {
	if s.config == nil || s.config.Server == nil || s.config.Server.Http == nil {
		return nil
	}

	config := s.config.Server.Http
	var path = "/"
	if config.Path != nil {
		// Path 为 "/" 时构造出 "//"，会导致 HTTP 路由匹配与路径截取错位
		trimmed := strings.Trim(*config.Path, "/")
		if trimmed != "" {
			path = "/" + trimmed + "/"
		}
	}

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
	s.addr.Store(nil) // 监听地址变化后清空缓存，下次 GetServerAddr 重新解析
	hlog.Info(ctx, "start rpc server: %s", listen.Addr())

	// 按配置选择传输层
	if s.config.GetTransport() == TransportH2Framer {
		return s.startH2FramerServer(ctx, path, listen)
	}
	return s.startHttpServer(ctx, path, listen)
}

// startHttpServer 基于标准库 net/http 启动 RPC 服务
func (s *Service) startHttpServer(ctx context.Context, path string, listen net.Listener) error {
	config := s.config.Server.Http

	mux := http.NewServeMux()
	mux.Handle(path, hrpc.NewHttpServer(path, s.rpcServer))

	tlsConfig, err := s.newTlsConfig(config)
	if err != nil {
		return err
	}

	server := &http.Server{
		Handler:   mux,
		TLSConfig: tlsConfig,
	}
	s.httpServer.Store(server)
	go func() {
		err := server.ServeTLS(listen, "", "")
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			hlog.Error(ctx, "start https rpc server failed: %s", err)
			return
		}
	}()
	return nil
}

// startH2FramerServer 基于 hrpc.H2Framer 启动 RPC 服务
func (s *Service) startH2FramerServer(ctx context.Context, path string, listen net.Listener) error {
	config := s.config.Server.Http

	tlsConfig, err := s.newTlsConfig(config)
	if err != nil {
		return err
	}

	// H2Framer 服务端为 prior-knowledge h2，由 TLS 层完成 ALPN h2 协商
	h2Server := hrpc.NewH2Server(path, s.rpcServer)
	s.h2Server.Store(h2Server)
	go func() {
		err := h2Server.Serve(tls.NewListener(listen, tlsConfig))
		if err != nil {
			hlog.Error(ctx, "start h2 framer rpc server failed: %s", err)
			return
		}
	}()
	return nil
}

// newTlsConfig 构造服务端 TLS 配置：优先加载 Crt/Key 证书，否则生成自签名证书。
// 证书与自签名场景下客户端均无法验证，故客户端统一跳过证书校验。
func (s *Service) newTlsConfig(config *Http) (*tls.Config, error) {
	if config.Crt != nil && config.Key != nil && *config.Crt != "" && *config.Key != "" {
		cert, err := tls.LoadX509KeyPair(*config.Crt, *config.Key)
		if err != nil {
			return nil, err
		}
		return &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{http2Proto}}, nil
	}

	// 生成自签名证书
	privateKey, err := s.generatePrivateKey()
	if err != nil {
		return nil, err
	}
	cert, err := s.generateSelfSignedCert(privateKey)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{http2Proto}}, nil
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

// GetServerAddr 获取服务地址。
// 多网卡机器上每次遍历网卡解析可能得到不同地址，首次解析成功后缓存，
// 保证 Register 写入的 key 与 Deregister 删除的 prefix 使用一致地址。
func (s *Service) GetServerAddr() string {
	if addr := s.addr.Load(); addr != nil && *addr != "" {
		return *addr
	}

	listen := s.listen.Load()
	if listen == nil {
		return ""
	}
	addr, port, err := net.SplitHostPort((*listen).Addr().String())
	if err != nil {
		return ""
	}
	if addr != "::" {
		result := addr + ":" + port
		s.addr.Store(&result)
		return result
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

		result := ip.String() + ":" + port
		s.addr.Store(&result)
		return result
	}

	result := addrs[0].(*net.IPNet).IP.String() + ":" + port
	s.addr.Store(&result)
	return result
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

	install, ok := s.install[strings.Trim(info.Path, "/")]
	if !ok {
		return nil
	}

	err = s.delHttpClient(install, info.Host)
	if err != nil {
		return err
	}
	//
	//if s.onDeleteInfo != nil {
	//	s.onDeleteInfo(info)
	//}

	return nil
}

// newEncoder 按配置返回编解码器（服务端与客户端共用）
func (s *Service) newEncoder() hrpc.Encoder {
	if s.config != nil && s.config.GetCodec() == CodecHBuf {
		return hrpc.NewHBufEncode()
	}
	return hrpc.NewJsonEncode()
}

// newDecoder 按配置返回解码器（服务端与客户端共用）
func (s *Service) newDecoder() hrpc.Decoder {
	if s.config != nil && s.config.GetCodec() == CodecHBuf {
		return hrpc.NewHBufDecode()
	}
	return hrpc.NewJsonDecode()
}

// delHttpClient
func (s *Service) delHttpClient(install *ServerInfo, host string) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if val, ok := s.clients[install.name]; ok {
		// 复制一份新切片，避免直接在旧切片上修改导致 GetClient 并发冲突
		newList := hutl.Filter(val.list, func(router Router) bool {
			return router.isLocal || (!router.isLocal && router.host != host)
		})

		// 替换成新切片指针
		val.list = newList
	}
	delete(s.httpClient, host)
	return nil
}

// addHttpClient 增加HTTP客户端
func (s *Service) addHttpClient(install *ServerInfo, addr *url.URL) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	connect, ok := s.httpClient[addr.Host]
	if !ok {
		// 按配置选择传输层与客户端编解码器。
		// 服务端证书为自签名或自定义，客户端统一跳过证书校验。
		var httpOptions []hrpc.HttpClientOption
		if s.config != nil && s.config.GetTransport() == TransportH2Framer {
			httpOptions = append(httpOptions,
				hrpc.WithInsecureSkipVerify(true),
				hrpc.WithH2FramerTransport(hrpc.NewH2FramerTransport(hrpc.WithFramerInsecureSkipVerify(true))),
			)
		} else {
			httpOptions = append(httpOptions, hrpc.WithInsecureSkipVerify(true))
		}
		connect = hrpc.NewClient(
			hrpc.NewHttpClient(addr.String(), httpOptions...).Request,
			hrpc.WithClientEncoder(s.newEncoder()),
			hrpc.WithClientDecode(s.newDecoder()),
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
		isLocal: false,
		host:    addr.Host,
	})

	s.checkSubscribe()
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
		isLocal: true,
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

// 检测服务是否未完成订阅。调用方需持有 s.lock。
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
	// 停止 keepSession 守护协程
	s.keepMu.Lock()
	if s.keepCancel != nil {
		s.keepCancel()
		s.keepCancel = nil
	}
	s.keepMu.Unlock()

	// 关闭 H2Framer 服务（内部会等待在途请求完成）
	if h2Server := s.h2Server.Swap(nil); h2Server != nil {
		_ = h2Server.Shutdown(ctx)
	}
	// 关闭监听器，使 H2Framer Serve 的 Accept 循环退出
	if listen := s.listen.Load(); listen != nil {
		_ = (*listen).Close()
	}

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
			if router.isLocal {
				return router.Init, index
			}
		}
		return dispatch(list, index)
	}
}
