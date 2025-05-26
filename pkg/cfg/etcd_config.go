package cfg

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	clientv3 "go.etcd.io/etcd/client/v3"
	"strings"
)

type etcdConfig struct {
	endpoints []string
	hostname  string
	client    *clientv3.Client
	value     string
	onChange  func(c string)
	keyVal    map[string]any
}

func (c *etcdConfig) OnChange(call func(value string)) error {
	if 0 == len(c.value) {
		get, err := c.client.Get(context.TODO(), c.hostname+"__config")
		if err != nil {
			return err
		}
		if 0 == len(get.Kvs) {
			return erro.NewError("get config file error")
		}
		c.value = string(get.Kvs[0].Value)
		if nil != call {
			config, err := generateConfig(c.value, c.keyVal)
			if err != nil {
				erro.PrintStack(err)
				return err
			}
			call(config)
		}
	}
	c.onChange = call
	return nil
}

func NewEtcdConfig(hostname string, endpoints string, val map[string]any) Watch {
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
		hlog.Error("Etcd server connection failed, please check the configuration is correct:", err)
	}
	ret.client = client
	return ret
}

func (c *etcdConfig) Close() error {
	return c.client.Close()
}

func (c *etcdConfig) Watch() error {
	rch := c.client.Watch(context.Background(), "config")
	for wresp := range rch {
		for _, ev := range wresp.Events {
			var value string
			if clientv3.EventTypeDelete == ev.Type {
				value = ""
			} else {
				value = string(ev.Kv.Value)
			}
			if value != c.value && nil != c.onChange {
				hlog.Info("config file change:", value)
				config, err := generateConfig(value, c.keyVal)
				if err != nil {
					erro.PrintStack(err)
					return err
				}
				hlog.Debug("config change:" + config)
				c.onChange(config)
			}
			c.value = value
		}
	}
	hlog.Flush()
	return nil
}
