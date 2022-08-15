package cache

import (
	"context"
	"github.com/garyburd/redigo/redis"
	"log"
	"reflect"
	"time"
)

type Config struct {
	Network     *string `yaml:"network"`      // 网络类型
	Address     *string `yaml:"address"`      // Redis 服务器地址
	Password    *string `yaml:"password"`     // 密码
	MaxIdle     *int    `yaml:"max_idle"`     // 最大空闲链接数 默认8
	MaxActive   *int    `yaml:"max_active"`   // 表示和数据库的最大链接数， 默认0 表示没有限制
	IdleTimeout *int    `yaml:"idle_timeout"` // 最大空闲时间  默认0100ms
}

func (con *Config) CheckConfig() int {
	errCount := 0
	if nil == con.Network || !("tcp" == *con.Network) {
		errCount++
		log.Println("未找到Redis支持的网络类型，请使用 tcp")
	}
	if nil == con.Address || "" == *con.Address {
		errCount++
		log.Println("未找到Redis服务器地址")

	}

	conn, err := redis.Dial(*con.Network, *con.Address)
	if err != nil {
		errCount++

		log.Fatalln("Redis链接失败，请检查配置是否正确", err)

	}
	defer func(c redis.Conn) {
		_ = c.Close()
	}(conn)

	if nil != con.Password && 0 != len(*con.Password) {
		_, err := conn.Do("AUTH", *con.Password)
		if err != nil {
			errCount++

			log.Println("Redis 认证失败，请检查密码是否正确", err)

		}
	}
	log.Println("Redis 检查：Ok")
	return errCount
}

type cacheContext struct {
	context.Context
	cache *Cache
	con   redis.Conn
}

func (d *cacheContext) Value(key interface{}) interface{} {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

func (d *cacheContext) Done() <-chan struct{} {
	return d.Context.Done()
}

var cacheType = reflect.TypeOf(&cacheContext{})

func GET(ctx context.Context) redis.Conn {
	var ret = ctx.Value(cacheType)
	if nil == ret {
		return nil
	}
	if nil != ret.(*cacheContext).con {
		return ret.(*cacheContext).con
	}
	ret.(*cacheContext).con = ret.(*cacheContext).cache.pool.Get()
	go func() {
		if nil != ret.(*cacheContext).con {
			select {
			case <-ctx.Done():
				ret.(*cacheContext).con.Close()
			}
		}
	}()
	return ret.(*cacheContext).con
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
				return redis.Dial(*con.Network, *con.Address, option...)
			},
		},
	}
}

func (c *Cache) OnFilter(ctx context.Context) (context.Context, error) {
	if nil == ctx.Value(cacheType) {
		ctx = &cacheContext{
			ctx,
			c,
			nil,
		}
	}
	return ctx, nil
}
