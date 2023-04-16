package config

import (
	"context"
	"fmt"
	clientv3 "go.etcd.io/etcd/client/v3"
	"log"
	"strings"
)

type etcdConfig struct {
	endpoints []string
	hostname  string
}

func NewEtcdConfig(hostname string, endpoints string) Watch {
	return &etcdConfig{
		hostname:  hostname,
		endpoints: strings.Split(endpoints, ","),
	}
}

func (c *etcdConfig) Watch() {
	etc := clientv3.Config{
		Endpoints: c.endpoints,
	}
	client, err := clientv3.New(etc)
	if err != nil {
		log.Fatalln("Etcd服务器连接失败，请检查配置是否正确", err)
	}
	defer client.Close()

	rch := client.Watch(context.Background(), "config")
	for wresp := range rch {
		for _, ev := range wresp.Events {

			fmt.Printf("Type: %s Key:%s Value:%s\n", ev.Type, ev.Kv.Key, ev.Kv.Value)
		}
	}
}
