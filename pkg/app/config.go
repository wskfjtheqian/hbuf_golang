package app

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/nats"
	"github.com/wskfjtheqian/hbuf_golang/pkg/redis"
)

type Config struct {
	Nats  *nats.Config  `yaml:"nats"`
	Etcd  *etcd.Config  `yaml:"etcd"`
	Redis *redis.Config `yaml:"redis"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	valid := true
	if !c.Nats.Validate() {
		valid = false
	}
	if !c.Etcd.Validate() {
		valid = false
	}
	if !c.Redis.Validate() {
		valid = false
	}
	return valid
}

// Equal 判断两个Config是否相同
func (c *Config) Equal(other *Config) bool {
	if !c.Nats.Equal(other.Nats) {
		return false
	}
	if !c.Etcd.Equal(other.Etcd) {
		return false
	}
	if !c.Redis.Equal(other.Redis) {
		return false
	}
	return true
}
