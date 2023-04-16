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

func NewCache(con *Config) *Cache {
	ret := Cache{
		config: con,
	}
	ret.config.OnChange(ret.onConfig)
	return &ret
}

func (c *Cache) onConfig(v *ConfigValue) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if nil == v {
		if nil != c.pool {
			c.pool.Close()
		}
		c.pool = nil
		return
	}

	maxIdle := 8
	if nil != v.MaxIdle {
		maxIdle = *v.MaxIdle
	}

	maxActive := 16
	if nil != v.MaxActive {
		maxActive = *v.MaxActive
	}

	idleTimeout := time.Millisecond * 100
	if nil != v.IdleTimeout {
		idleTimeout = time.Millisecond * time.Duration(*v.IdleTimeout)
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
			if nil != v.Password && 0 < len(*v.Password) {
				option = append(option, redis.DialPassword(*v.Password))
			}
			option = append(option, redis.DialDatabase(v.Db))
			return redis.Dial(*v.Network, *v.Address, option...)
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
