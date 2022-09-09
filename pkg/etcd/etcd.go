package etc

import (
	"context"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
	"log"
	"reflect"
)

type contextValue struct {
	client  *clientv3.Client
	session *concurrency.Session
}

func (v *contextValue) GetSession(ctx context.Context) (*concurrency.Session, error) {
	if nil != v.session {
		return v.session, nil
	}
	var err error
	v.session, err = concurrency.NewSession(v.client)
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

func (d *Context) Value(key interface{}) interface{} {
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
}

func NewEtcd(config *Config) *Etcd {
	c := clientv3.Config{
		Endpoints: config.Endpoints,
	}
	if nil != config.DialTimeout {
		c.DialTimeout = *config.DialTimeout
	}
	client, err := clientv3.New(c)
	if err != nil {
		log.Fatalln("Etcd服务器连接失败，请检查配置是否正确", err)
	}
	return &Etcd{
		client: client,
	}
}

func (d *Etcd) OnFilter(ctx context.Context) (context.Context, error) {
	if nil == ctx.Value(cType) {
		ctx = &Context{
			ctx,
			&contextValue{
				client: d.client,
			},
		}
	}
	return ctx, nil
}
