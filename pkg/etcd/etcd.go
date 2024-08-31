package etc

import (
	"context"
	"github.com/garyburd/redigo/redis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
	"reflect"
	"sync"
)

type contextValue struct {
	client  *clientv3.Client
	session *concurrency.Session
}

func (v *contextValue) GetSession(ctx context.Context, opts ...concurrency.SessionOption) (*concurrency.Session, error) {
	if nil == v.client {
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
				hlog.Exit(err)
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

func NewEtcd() *Etcd {
	ret := &Etcd{}
	return ret
}

func (d *Etcd) SetConfig(config *Config) {
	d.lock.Lock()
	defer d.lock.Unlock()
	if nil == config {
		if nil != d.client {
			d.client.Close()
		}
		d.client = nil
		d.config = nil
		return
	}
	if nil != d.config && d.config.Yaml() == config.Yaml() {
		return
	}
	d.config = config

	c := clientv3.Config{
		Endpoints: config.Endpoints,
	}
	if nil != config.DialTimeout {
		c.DialTimeout = *config.DialTimeout
	}
	client, err := clientv3.New(c)
	if err != nil {
		hlog.Exit("Etcd服务器连接失败，请检查配置是否正确", err)
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

func (d *Etcd) GetClient() *clientv3.Client {
	return d.client
}
