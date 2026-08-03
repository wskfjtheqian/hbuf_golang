package hneo4j

import (
	"context"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
)

// WithContext 给上下文添加 NEO4J 连接
func WithContext(ctx context.Context, n *Neo4j) context.Context {
	return &Context{
		Context: ctx,
		neo4j:   n,
	}
}

// Context 定义了 NEO4J 的上下文
type Context struct {
	context.Context
	neo4j *Neo4j
}

var contextType = reflect.TypeOf(&Context{})

// Value 返回Context的value
func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

// FromContext 从上下文中获取 NEO4J 连接
func FromContext(ctx context.Context) (n *Neo4j, ok bool) {
	val := ctx.Value(contextType)
	if val == nil {
		return nil, false
	}
	return val.(*Context).neo4j, true
}

///////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func NewNeo4j() *Neo4j {
	return &Neo4j{}
}

type Neo4j struct {
	config *Config
	driver atomic.Pointer[neo4j.DriverWithContext]
}

func (n *Neo4j) SetConfig(cfg *Config) error {
	ctx := context.Background()

	if n.config.Equal(cfg) {
		return nil
	}
	old := n.driver.Load()
	defer func() {
		if old != nil {
			<-time.After(time.Second * 30)
			_ = (*old).Close(ctx)
			hlog.Info("old neo4j client closed")
		}
	}()

	if cfg == nil {
		if old != nil {
			conn := n.driver.Swap(nil)
			_ = (*conn).Close(ctx)
		}
		n.config = nil
		return nil
	}

	n.config = cfg
	options := make([]func(*config.Config), 0)

	// 连接池最大连接数
	options = append(options, func(c *config.Config) {
		if cfg.MaxConnectionPoolSize != nil && *cfg.MaxConnectionPoolSize > 0 {
			c.MaxConnectionPoolSize = *cfg.MaxConnectionPoolSize
		}
	})
	// 获取连接超时
	options = append(options, func(c *config.Config) {
		c.ConnectionAcquisitionTimeout = durationVal(cfg.ConnectionAcquisitionTimeout, DefaultConnectionAcquisitionTimeout)
	})
	// 连接最大存活时间（<=0 表示永不过期，Neo4j driver 默认 1h）
	options = append(options, func(c *config.Config) {
		c.MaxConnectionLifetime = durationVal(cfg.MaxConnectionLifetime, DefaultMaxConnectionLifetime)
	})
	// Socket 连接超时
	options = append(options, func(c *config.Config) {
		c.SocketConnectTimeout = durationVal(cfg.SocketConnectTimeout, DefaultSocketConnectTimeout)
	})
	// TCP KeepAlive
	options = append(options, func(c *config.Config) {
		c.SocketKeepalive = boolVal(cfg.SocketKeepalive, true)
	})

	driver, err := neo4j.NewDriverWithContext(*cfg.Addr,
		neo4j.BasicAuth(*cfg.Username, *cfg.Password, ""),
		options...,
	)
	if err != nil {
		return herror.Wrap(err)
	}

	hlog.Info("neo4j client connected")
	n.driver.Store(&driver)

	return nil
}

// NewMiddleware 创建中间件
func (n *Neo4j) NewMiddleware() hrpc.Middleware {
	return func(next hrpc.Handler) hrpc.Handler {
		return func(ctx context.Context, req any) (any, error) {
			return next(WithContext(ctx, n), req)
		}
	}
}

func (n *Neo4j) Get(ctx context.Context) neo4j.SessionWithContext {
	return (*n.driver.Load()).NewSession(ctx, neo4j.SessionConfig{DatabaseName: *n.config.Database})
}
