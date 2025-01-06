package redis

import (
	"context"
	"github.com/go-redis/redis/v8"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
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
	if cfg.Addr != "" {
		options.Addr = cfg.Addr
	}
	if cfg.Password != "" {
		options.Password = cfg.Password
	}
	if cfg.DB != 0 {
		options.DB = cfg.DB
	}
	if cfg.MaxRetries != 0 {
		options.MaxRetries = cfg.MaxRetries
	}
	if cfg.MinRetryBackoff != 0 {
		options.MinRetryBackoff = cfg.MinRetryBackoff
	}
	if cfg.MaxRetryBackoff != 0 {
		options.MaxRetryBackoff = cfg.MaxRetryBackoff
	}
	if cfg.DialTimeout != 0 {
		options.DialTimeout = cfg.DialTimeout
	}
	if cfg.ReadTimeout != 0 {
		options.ReadTimeout = cfg.ReadTimeout
	}
	if cfg.WriteTimeout != 0 {
		options.WriteTimeout = cfg.WriteTimeout
	}
	if cfg.PoolSize != 0 {
		options.PoolSize = cfg.PoolSize
	}
	if cfg.MinIdleConns != 0 {
		options.MinIdleConns = cfg.MinIdleConns
	}
	if cfg.MaxConnAge != 0 {
		options.MaxConnAge = cfg.MaxConnAge
	}
	if cfg.PoolTimeout != 0 {
		options.PoolTimeout = cfg.PoolTimeout
	}
	client := redis.NewClient(options)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return erro.Wrap(err)
	}

	r.conn.Store(client)

	return nil
}
