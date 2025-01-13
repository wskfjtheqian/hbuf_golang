package nats

import (
	"context"
	"encoding/json"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// 协议名称
const ProtocolName = "hbuf-rpc://"

// RegisterInfo 定义了服务注册信息
type RegisterInfo struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
	Port int    `json:"port"`
	TTL  int    `json:"ttl"`
}

// NewService 创建一个新的Service实例
func NewService(etcd *etcd.Etcd) *Service {
	return &Service{
		etcd:    etcd,
		install: make(map[string]struct{}),
	}
}

// Service 定义了一个服务接口
type Service struct {
	config    *Config
	etcd      *etcd.Etcd
	lease     atomic.Pointer[clientv3.LeaseGrantResponse]
	listen    atomic.Pointer[net.Listener]
	rpcServer *rpc.Server
	install   map[string]struct{}
}

// SetConfig 设置配置
func (s *Service) SetConfig(cfg *Config) error {
	if s.config.Equal(cfg) {
		return nil
	}
	if cfg == nil {
		err := s.Deregister(context.Background())
		if err != nil {
			hlog.Error("deregister service failed: ", err)
		}
		s.config = nil
		return nil
	}

	s.config = cfg
	err := s.startRpcServer()
	if err != nil {
		return err
	}

	err = s.Register(context.Background())
	if err != nil {
		return err
	}
	go func() {
		err := s.Discovery(context.Background())
		if err != nil {
			hlog.Error("discovery service failed: ", err)
		}
	}()
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
			hlog.Error("revoke lease failed: ", err)
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

	for key, _ := range s.install {
		// 构造服务注册信息
		name := ProtocolName + s.GetServerAddr() + "/" + key
		value, err := json.Marshal(&RegisterInfo{})
		if err != nil {
			return err
		}

		// 注册服务到etcd
		_, err = client.Put(ctx, name, string(value), clientv3.WithLease(lease.ID))
		if err != nil {
			return err
		}

		hlog.Info("register service success: ", key)
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
			hlog.Error("revoke lease failed: ", err)
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
		err := s.addClient(v, true)
		if err != nil {
			hlog.Error("add client failed: ", err)
		}
	}

	// 监听服务变化
	watchCh := client.Watch(ctx, name, clientv3.WithPrefix())
	for w := range watchCh {
		for _, ev := range w.Events {
			if ev.Type == clientv3.EventTypePut {
				err := s.addClient(ev.Kv, true)
				if err != nil {
					hlog.Error("add client failed: ", err)
				}
			} else if ev.Type == clientv3.EventTypeDelete {
				hlog.Info("service deregister: ", ev.Kv.Key)
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
	var path string
	if config.Path != nil {
		path = *config.Path
	}
	path = "/" + strings.Trim(path, "/") + "/"

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
	hlog.Info("start http server: ", listen.Addr())

	go func() {
		if config.Crt != nil && config.Key != nil && *config.Crt != "" && *config.Key != "" {
			// 开启https服务
			err := http.ServeTLS(listen, mux, *config.Crt, *config.Key)
			if err != nil {
				hlog.Error("start https server failed: ", err)
				return
			}
		} else {
			// 开启http服务
			err := http.Serve(listen, mux)
			if err != nil {
				hlog.Error("start http server failed: ", err)
				return
			}
		}
	}()
	return nil
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

// addClient 增加客户端
func (s *Service) addClient(v *mvccpb.KeyValue, isHttp bool) error {
	parse, err := url.Parse(string(v.Key))
	if err != nil {
		return erro.Wrap(err)
	}

	if parse.Path == "" {
		return nil
	}

	if _, ok := s.install[parse.Path]; ok {
		return nil
	}

	info := &RegisterInfo{}
	err = json.Unmarshal(v.Value, info)
	if err != nil {
		return erro.Wrap(err)
	}

	return nil
}

//
//type RcpClient interface {
//	getClient() rpc.Init
//}
//
//type localRpcClient struct {
//	client rpc.Init
//}
//
//func newLocalRpcClient(router rpc.ServerRouter) RcpClient {
//	return &localRpcClient{
//		client: router.GetServer(),
//	}
//}
//
//func (c *localRpcClient) getClient() rpc.Init {
//	return c.client
//}
//
//type httpRpcClient struct {
//	client rpc.Init
//}
//
//func newHttpRpcClient(url string, call CallClient) RcpClient {
//	client := rpc.NewClientHttp(url)
//	jsonClient := rpc.NewJsonClient(client)
//	return &httpRpcClient{
//		client: call(jsonClient),
//	}
//}
//
//func (c *httpRpcClient) getClient() rpc.Init {
//	return c.client
//}
