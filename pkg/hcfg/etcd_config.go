package hcfg

import (
	"context"
	"strings"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type etcdConfig struct {
	endpoints []string
	hostname  string
	client    *clientv3.Client
	value     string
	onChange  func(ctx context.Context, c string)
	keyVal    map[string]any
}

func (c *etcdConfig) OnChange(ctx context.Context, call func(ctx context.Context, value string)) error {
	if 0 == len(c.value) {
		get, err := c.client.Get(ctx, c.hostname+"__config")
		if err != nil {
			return err
		}
		if 0 == len(get.Kvs) {
			return herror.NewError("get config file error")
		}
		c.value = string(get.Kvs[0].Value)
		if nil != call {
			config, err := generateConfig(ctx, c.value, c.keyVal)
			if err != nil {
				herror.PrintStack(ctx, err)
				return err
			}
			call(ctx, config)
		}
	}
	c.onChange = call
	return nil
}

func NewEtcdConfig(ctx context.Context, hostname string, endpoints string, val map[string]any) Watch {
	ret := &etcdConfig{
		hostname:  hostname,
		endpoints: strings.Split(endpoints, ","),
		keyVal:    val,
	}
	etc := clientv3.Config{
		Endpoints: ret.endpoints,
	}
	client, err := clientv3.New(etc)
	if err != nil {
		hlog.Error(ctx, "Etcd server connection failed, please check the configuration is correct:%s", err)
	}
	ret.client = client
	return ret
}

func (c *etcdConfig) Close(ctx context.Context) error {
	return c.client.Close()
}

func (c *etcdConfig) Watch(ctx context.Context) error {
	rch := c.client.Watch(ctx, "config")
	for wResp := range rch {
		ctx = hlog.WithContext(ctx, "")
		for _, ev := range wResp.Events {
			var value string
			if clientv3.EventTypeDelete == ev.Type {
				value = ""
			} else {
				value = string(ev.Kv.Value)
			}
			if value != c.value && nil != c.onChange {
				hlog.Info(ctx, "config file change: %s", value)
				config, err := generateConfig(ctx, value, c.keyVal)
				if err != nil {
					herror.PrintStack(ctx, err)
					return err
				}
				hlog.Debug(ctx, "config change:%s"+config)
				c.onChange(ctx, config)
			}
			c.value = value
		}
	}
	hlog.Flush()
	return nil
}
