package etcd

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	clientv3 "go.etcd.io/etcd/client/v3"
	"reflect"
	"sync/atomic"
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
	client atomic.Pointer[clientv3.Client]
	config *Config
}

// SetConfig 设置etcd的配置
func (e *Etcd) SetConfig(cfg *Config) error {
	if e.config.Equal(cfg) {
		return nil
	}
	if cfg == nil {
		if e.client.Load() != nil {
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
	if cfg.DialTimeout > 0 {
		c.DialTimeout = cfg.DialTimeout
	}
	client, err := clientv3.New(c)
	if err != nil {
		return erro.Wrap(err)
	}

	e.client.Store(client)
	return err
}

// GetClient 获取etcd的客户端
func (e *Etcd) GetClient() (*clientv3.Client, error) {
	if e.client.Load() == nil {
		return nil, erro.NewError("not found etcd client")
	}
	return e.client.Load(), nil
}

// NewMiddleware 创建中间件
func (e *Etcd) NewMiddleware() rpc.HandlerMiddleware {
	return func(next rpc.Handler) rpc.Handler {
		return func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
			return next(WithContext(ctx, n), req)
		}
	}
}
