package redis

import (
	"context"
	"github.com/go-redis/redis/v8"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"reflect"
	"sync/atomic"
)

// WithContext 给上下文添加 REDIS 连接
func WithContext(ctx context.Context, n *Redis) context.Context {
	return &Context{
		Context: ctx,
		redis:   n,
	}
}

// Context 定义了 REDIS 的上下文
type Context struct {
	context.Context
	redis *Redis
}

var contextType = reflect.TypeOf(&Context{})

// Value 返回Context的value
func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

// FromContext 从上下文中获取 REDIS 连接
func FromContext(ctx context.Context) (n *Redis, ok bool) {
	val := ctx.Value(contextType)
	if val == nil {
		return nil, false
	}
	return val.(*Context).redis, true
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// NewRedis 创建一个 Redis 实例
func NewRedis() *Redis {
	return &Redis{}
}

// Redis 封装了 Redis 客户端的操作
type Redis struct {
	config *Config
	conn   atomic.Pointer[redis.Client]
}

// SetConfig 设置 Redis 配置
func (r *Redis) SetConfig(cfg *Config) error {
	if r.config.Equal(cfg) {
		return nil
	}
	if cfg == nil {
		if r.conn.Load() != nil {
			conn := r.conn.Swap(nil)
			_ = conn.Close()
		}
		r.config = nil
		return nil
	}

	r.config = cfg
	options := &redis.Options{}
	if cfg.Network != nil {
		options.Network = *cfg.Network
	}
	if cfg.Addr != nil {
		options.Addr = *cfg.Addr
	}
	if cfg.Username != nil {
		options.Username = *cfg.Username
	}
	if cfg.Password != nil {
		options.Password = *cfg.Password
	}
	if cfg.DB != nil {
		options.DB = *cfg.DB
	}
	if cfg.MaxRetries != nil {
		options.MaxRetries = *cfg.MaxRetries
	}
	if cfg.MinRetryBackoff != nil {
		options.MinRetryBackoff = *cfg.MinRetryBackoff
	}
	if cfg.MaxRetryBackoff != nil {
		options.MaxRetryBackoff = *cfg.MaxRetryBackoff
	}
	if cfg.DialTimeout != nil {
		options.DialTimeout = *cfg.DialTimeout
	}
	if cfg.ReadTimeout != nil {
		options.ReadTimeout = *cfg.ReadTimeout
	}
	if cfg.WriteTimeout != nil {
		options.WriteTimeout = *cfg.WriteTimeout
	}
	if cfg.PoolFIFO != nil {
		options.PoolFIFO = *cfg.PoolFIFO
	}
	if cfg.PoolSize != nil {
		options.PoolSize = *cfg.PoolSize
	}
	if cfg.MinIdleConns != nil {
		options.MinIdleConns = *cfg.MinIdleConns
	}
	if cfg.MaxConnAge != nil {
		options.MaxConnAge = *cfg.MaxConnAge
	}
	if cfg.PoolTimeout != nil {
		options.PoolTimeout = *cfg.PoolTimeout
	}
	if cfg.IdleTimeout != nil {
		options.IdleTimeout = *cfg.IdleTimeout
	}
	if cfg.IdleCheckFrequency != nil {
		options.IdleCheckFrequency = *cfg.IdleCheckFrequency
	}
	client := redis.NewClient(options)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return erro.Wrap(err)
	}

	r.conn.Store(client)

	return nil
}

// NewMiddleware 创建中间件
func (r *Redis) NewMiddleware() rpc.HandlerMiddleware {
	return func(next rpc.Handler) rpc.Handler {
		return func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
			return next(WithContext(ctx, r), req)
		}
	}
}
