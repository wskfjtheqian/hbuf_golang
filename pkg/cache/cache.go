package cache

import (
	"context"
	"github.com/garyburd/redigo/redis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"reflect"
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
	pool *redis.Pool
}

func NewCache(con *Config) *Cache {
	maxIdle := 8
	if nil != con.MaxIdle {
		maxIdle = *con.MaxIdle
	}

	maxActive := 16
	if nil != con.MaxActive {
		maxActive = *con.MaxActive
	}

	idleTimeout := time.Millisecond * 100
	if nil != con.IdleTimeout {
		idleTimeout = time.Millisecond * time.Duration(*con.IdleTimeout)
	}

	return &Cache{
		pool: &redis.Pool{
			MaxIdle:     maxIdle,
			MaxActive:   maxActive,
			IdleTimeout: idleTimeout,
			Dial: func() (redis.Conn, error) {
				option := make([]redis.DialOption, 0)
				if nil != con.Password && 0 < len(*con.Password) {
					option = append(option, redis.DialPassword(*con.Password))
				}
				option = append(option, redis.DialDatabase(con.Db))
				return redis.Dial(*con.Network, *con.Address, option...)
			},
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
