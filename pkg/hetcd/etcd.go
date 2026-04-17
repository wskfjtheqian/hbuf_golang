package hetcd

import (
	"context"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"reflect"
	"sync/atomic"
	"time"
)

// WithContext 给上下文添加 NATS 连接
func WithContext(ctx context.Context, n *Etcd) context.Context {
	return &Context{
		Context: ctx,
		etcd:    n,
	}
}

// Context 定义了 NATS 的上下文
type Context struct {
	context.Context
	etcd *Etcd
}

var contextType = reflect.TypeOf(&Context{})

// Value 返回Context的value
func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

// FromContext 从上下文中获取Etcd对象
func FromContext(ctx context.Context) (e *Etcd, ok bool) {
	val := ctx.Value(contextType)
	if val == nil {
		return nil, false
	}
	return val.(*Context).etcd, true
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// NewEtcd 创建一个Etcd对象
func NewEtcd() *Etcd {
	ret := &Etcd{}
	return ret
}

// Etcd 封装了Etcd的连接和操作
type Etcd struct {
	client  atomic.Pointer[clientv3.Client]
	session atomic.Pointer[concurrency.Session]
	config  *Config
}

// SetConfig 设置etcd的配置
func (e *Etcd) SetConfig(cfg *Config) error {
	if e.config.Equal(cfg) {
		return nil
	}

	old := e.client.Load()
	oldSession := e.session.Load()
	defer func() {
		if oldSession != nil || old != nil {
			<-time.After(time.Second * 30)
		}
		if oldSession != nil {
			_ = oldSession.Close()
			hlog.Info("old etcd session closed")
		}
		if old != nil {

			_ = old.Close()
			hlog.Info("old etcd client closed")
		}
	}()

	if cfg == nil {
		if oldSession != nil {
			session := e.session.Swap(nil)
			_ = session.Close()
			hlog.Info("old etcd session closed")
		}
		if old != nil {
			conn := e.client.Swap(nil)
			_ = conn.Close()
		}
		e.config = nil
		return nil
	}

	e.config = cfg

	c := clientv3.Config{
		Endpoints: cfg.Endpoints,
	}
	if cfg.Endpoints != nil {
		c.Endpoints = cfg.Endpoints
	}
	if cfg.AutoSyncInterval != nil {
		c.AutoSyncInterval = *cfg.AutoSyncInterval
	}
	if cfg.DialTimeout != nil {
		c.DialTimeout = *cfg.DialTimeout
	}
	if cfg.DialKeepAliveTime != nil {
		c.DialKeepAliveTime = *cfg.DialKeepAliveTime
	}
	if cfg.DialKeepAliveTimeout != nil {
		c.DialKeepAliveTimeout = *cfg.DialKeepAliveTimeout
	}
	if cfg.MaxCallSendMsgSize != nil {
		c.MaxCallSendMsgSize = *cfg.MaxCallSendMsgSize
	}
	if cfg.MaxCallRecvMsgSize != nil {
		c.MaxCallRecvMsgSize = *cfg.MaxCallRecvMsgSize
	}
	if cfg.Username != nil {
		c.Username = *cfg.Username
	}
	if cfg.Password != nil {
		c.Password = *cfg.Password
	}
	if cfg.RejectOldCluster != nil {
		c.RejectOldCluster = *cfg.RejectOldCluster
	}
	if cfg.PermitWithoutStream != nil {
		c.PermitWithoutStream = *cfg.PermitWithoutStream
	}
	if cfg.MaxUnaryRetries != nil {
		c.MaxUnaryRetries = *cfg.MaxUnaryRetries
	}
	if cfg.BackoffWaitBetween != nil {
		c.BackoffWaitBetween = *cfg.BackoffWaitBetween
	}
	if cfg.BackoffJitterFraction != nil {
		c.BackoffJitterFraction = *cfg.BackoffJitterFraction
	}

	client, err := clientv3.New(c)
	if err != nil {
		return herror.Wrap(err)
	}

	ctx := context.Background()
	for _, endpoint := range client.Endpoints() {
		ctx1, _ := context.WithTimeout(ctx, time.Second*10)
		status, err := client.Status(ctx1, endpoint)
		if err != nil {
			hlog.Exit("dial etcd failed: %s", err)
		}
		hlog.Info("etcd endpoint: %s, isLearner: %t", endpoint, status.IsLearner)
	}
	hlog.Info("etcd client connected")

	e.client.Store(client)
	session, err := concurrency.NewSession(client)
	if err != nil {
		_ = client.Close()
		return err
	}
	e.session.Store(session)
	go e.watchSession(client)
	return err
}

// GetClient 获取etcd的客户端
func (e *Etcd) GetClient() (*clientv3.Client, error) {
	client := e.client.Load()
	if client == nil {
		return nil, herror.NewError("not found etcd client")
	}
	return client, nil
}

// NewMiddleware 创建中间件
func (e *Etcd) NewMiddleware() hrpc.Middleware {
	return func(next hrpc.Handler) hrpc.Handler {
		return func(ctx context.Context, req any) (any, error) {
			return next(WithContext(ctx, e), req)
		}
	}
}

func (e *Etcd) GetSession() (*concurrency.Session, error) {
	session := e.session.Load()
	if session == nil {
		return nil, herror.NewError("not found etcd session")
	}
	return session, nil
}

func (e *Etcd) watchSession(client *clientv3.Client) {
	for {
		<-e.session.Load().Done()

		for {
			session, err := concurrency.NewSession(client)
			if err == nil {
				e.session.Store(session)
				break
			}
			time.Sleep(time.Second)
		}
	}
}
