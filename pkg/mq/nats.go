package mq

import (
	"context"
	"github.com/garyburd/redigo/redis"
	"github.com/nats-io/nats.go"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"reflect"
	"strings"
	"sync"
)

type Context struct {
	context.Context
	nats *Nats
}

var cType = reflect.TypeOf(&Context{})

func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

func (d *Context) Done() <-chan struct{} {
	return d.Context.Done()
}

func GET(ctx context.Context) *Nats {
	var ret = ctx.Value(cType)
	if nil == ret {
		return nil
	}
	return ret.(*Context).nats
}

type Nats struct {
	client *nats.Conn
	config *Config
	pool   *redis.Pool
	lock   sync.Mutex
}

func NewNats() *Nats {
	ret := &Nats{}
	return ret
}

func (d *Nats) SetConfig(config *Config) {
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

	var c []nats.Option
	if nil != config.Timeout {
		c = append(c, nats.Timeout(*config.Timeout))
	}
	if nil != config.DrainTimeout {
		c = append(c, nats.DrainTimeout(*config.DrainTimeout))
	}
	if nil != config.MaxReconnects {
		c = append(c, nats.MaxReconnects(*config.MaxReconnects))
	}
	if nil != config.MaxPingsOutstanding {
		c = append(c, nats.MaxPingsOutstanding(*config.MaxPingsOutstanding))
	}
	if nil != config.Name {
		c = append(c, nats.Name(*config.Name))
	}
	if nil != config.Username && nil != config.Password {
		c = append(c, nats.UserInfo(*config.Username, *config.Password))
	}
	if nil != config.CertFile && nil != config.KeyFile {
		c = append(c, nats.ClientCert(*config.CertFile, *config.KeyFile))
	}
	if nil != config.Token {
		c = append(c, nats.Token(*config.Token))
	}
	client, err := nats.Connect(strings.Join(config.Endpoints, ","), c...)
	if err != nil {
		hlog.Exit("Nats服务器连接失败，请检查配置是否正确", err)
	}
	d.client = client
}

func (d *Nats) OnFilter(ctx context.Context, data hbuf.Data, in *rpc.Filter, call rpc.FilterCall) (context.Context, hbuf.Data, error) {
	if nil == ctx.Value(cType) {
		ctx = &Context{
			ctx,
			d,
		}
	}
	return in.OnNext(ctx, data, call)
}

func (d *Nats) GetClient() *nats.Conn {
	return d.client
}

func (d *Nats) PublishMsg(Subject string, data []byte) error {
	err := d.client.PublishMsg(&nats.Msg{Subject: "hello", Data: data})
	if err != nil {
		return erro.Wrap(err)
	}
	return nil
}

func (d *Nats) Subscribe(Subject string, handler func(data []byte) error) error {
	_, err := d.client.Subscribe(Subject, func(msg *nats.Msg) {
		err := handler(msg.Data)
		if err != nil {
			hlog.Error("nats subscribe error:", err)
		}
	})
	if err != nil {
		return err
	}
	return nil
}
