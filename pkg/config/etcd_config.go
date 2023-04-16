package config

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	clientv3 "go.etcd.io/etcd/client/v3"
	"log"
	"strings"
)

type etcdConfig struct {
	endpoints []string
	hostname  string
	client    *clientv3.Client
	value     string
	onChange  func(c string)
}

func (c *etcdConfig) OnChange(call func(value string)) error {
	if 0 == len(c.value) {
		get, err := c.client.Get(context.TODO(), c.hostname+"__config")
		if err != nil {
			return err
		}
		if 0 == len(get.Kvs) {
			return erro.NewError("获得配置文件出错")
		}
		c.value = string(get.Kvs[0].Value)
		if nil != call {
			call(c.value)
		}
	}
	c.onChange = call
	return nil
}

func NewEtcdConfig(hostname string, endpoints string) Watch {
	ret := &etcdConfig{
		hostname:  hostname,
		endpoints: strings.Split(endpoints, ","),
	}
	etc := clientv3.Config{
		Endpoints: ret.endpoints,
	}
	client, err := clientv3.New(etc)
	if err != nil {
		log.Fatalln("Etcd服务器连接失败，请检查配置是否正确", err)
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
			if value != c.value {
				if nil != c.onChange {
					c.onChange(c.value)
				}
			}
		}
	}
	return nil
}
