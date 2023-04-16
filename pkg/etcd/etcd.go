package etc

import (
	"context"
	"github.com/garyburd/redigo/redis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
	"log"
	"reflect"
	"sync"
)

type contextValue struct {
	client  *clientv3.Client
	session *concurrency.Session
}

func (v *contextValue) GetSession(ctx context.Context, opts ...concurrency.SessionOption) (*concurrency.Session, error) {
	if nil != v.client {
		return nil, erro.NewError("未开启Etcd 功能")
	}
	if nil != v.session {
		return v.session, nil
	}
	var err error
	v.session, err = concurrency.NewSession(v.client, opts...)
	if nil != err {
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
			err := v.session.Close()
			if err != nil {
				log.Println(err)
			}
			v.session = nil
		}
	}()
	return v.session, nil
}

type Context struct {
	context.Context
	value *contextValue
}

var cType = reflect.TypeOf(&Context{})

func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d.value
	}
	return d.Context.Value(key)
}

func (d *Context) Done() <-chan struct{} {
	return d.Context.Done()
}

func GET(ctx context.Context) *contextValue {
	var ret = ctx.Value(cType)
	if nil == ret {
		return nil
	}
	return ret.(*contextValue)
}

type Etcd struct {
	client *clientv3.Client
	config *Config
	pool   *redis.Pool
	lock   sync.Mutex
}

func NewEtcd(config *Config) *Etcd {
	ret := &Etcd{
		config: config,
	}
	ret.config.OnChange(ret.onConfig)
	return ret
}

func (d *Etcd) onConfig(v *ConfigValue) {
	d.lock.Lock()
	defer d.lock.Unlock()
	if nil == v {
		if nil != d.client {
			d.client.Close()
		}
		d.client = nil
		return
	}
	c := clientv3.Config{
		Endpoints: v.Endpoints,
	}
	if nil != v.DialTimeout {
		c.DialTimeout = *v.DialTimeout
	}
	client, err := clientv3.New(c)
	if err != nil {
		log.Println("Etcd服务器连接失败，请检查配置是否正确", err)
	}
	d.client = client
}

func (d *Etcd) OnFilter(ctx context.Context, data hbuf.Data, in *rpc.Filter, call rpc.FilterCall) (context.Context, hbuf.Data, error) {
	if nil == ctx.Value(cType) {
		ctx = &Context{
			ctx,
			&contextValue{
				client: d.client,
			},
		}
	}
	return in.OnNext(ctx, data, call)
}
