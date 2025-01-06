package app

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/nats"
	"github.com/wskfjtheqian/hbuf_golang/pkg/redis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/sql"
)

type Config struct {
	Nats  *nats.Config  `yaml:"nats"`
	Etcd  *etcd.Config  `yaml:"etcd"`
	Redis *redis.Config `yaml:"redis"`
	Sql   *sql.Config   `yaml:"sql"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	valid := true
	if c.Nats == nil || !c.Nats.Validate() {
		valid = false
	}
	if c.Etcd == nil || !c.Etcd.Validate() {
		valid = false
	}
	if c.Redis == nil || !c.Redis.Validate() {
		valid = false
	}
	if c.Sql == nil || !c.Sql.Validate() {
		valid = false
	}
	return valid
}

// Equal 判断两个Config是否相同
func (c *Config) Equal(other *Config) bool {
	return c.Nats.Equal(other.Nats) &&
		c.Etcd.Equal(other.Etcd) &&
		c.Redis.Equal(other.Redis) &&
		c.Sql.Equal(other.Sql)

}
