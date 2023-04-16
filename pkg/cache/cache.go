package cache

import (
	"context"
	"github.com/garyburd/redigo/redis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"reflect"
	"sync"
	"time"
)

type Context struct {
	context.Context
	cache *Cache
	con   redis.Conn
}

func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

func (d *Context) Done() <-chan struct{} {
	return d.Context.Done()
}

var cType = reflect.TypeOf(&Context{})

func GET(ctx context.Context) redis.Conn {
	var ret = ctx.Value(cType)
	if nil == ret {
		return nil
	}
	if nil != ret.(*Context).con {
		return ret.(*Context).con
	}
	ret.(*Context).con = ret.(*Context).cache.pool.Get()
	go func() {
		if nil != ret.(*Context).con {
			select {
			case <-ctx.Done():
				ret.(*Context).con.Close()
			}
		}
	}()
	return ret.(*Context).con
}

type Cache struct {
	pool   *redis.Pool
	lock   sync.Mutex
	config *Config
}

func NewCache() *Cache {
	ret := Cache{}
	return &ret
}

func (c *Cache) SetConfig(config *Config) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if nil == config {
		if nil != c.pool {
			c.pool.Close()
		}
		c.pool = nil
		c.config = nil
		return
	}

	if nil != c.config && c.config.Yaml() == config.Yaml() {
		return
	}
	c.config = config

	maxIdle := 8
	if nil != config.MaxIdle {
		maxIdle = *config.MaxIdle
	}

	maxActive := 16
	if nil != config.MaxActive {
		maxActive = *config.MaxActive
	}

	idleTimeout := time.Millisecond * 100
	if nil != config.IdleTimeout {
		idleTimeout = time.Millisecond * time.Duration(*config.IdleTimeout)
	}

	if nil != c.pool {
		c.pool.Close()
	}
	c.pool = &redis.Pool{
		MaxIdle:     maxIdle,
		MaxActive:   maxActive,
		IdleTimeout: idleTimeout,
		Dial: func() (redis.Conn, error) {
			option := make([]redis.DialOption, 0)
			if nil != config.Password && 0 < len(*config.Password) {
				option = append(option, redis.DialPassword(*config.Password))
			}
			option = append(option, redis.DialDatabase(config.Db))
			return redis.Dial(*config.Network, *config.Address, option...)
		},
	}
}

func (c *Cache) OnFilter(ctx context.Context, data hbuf.Data, in *rpc.Filter, call rpc.FilterCall) (context.Context, hbuf.Data, error) {
	if nil == ctx.Value(cType) {
		ctx = &Context{
			ctx,
			c,
			nil,
		}
	}
	return in.OnNext(ctx, data, call)
}
