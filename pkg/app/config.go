package app

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/nats"
	"github.com/wskfjtheqian/hbuf_golang/pkg/redis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/service"
	"github.com/wskfjtheqian/hbuf_golang/pkg/sql"
)

type Config struct {
	Nats    *nats.Config    `yaml:"Nats"`
	Etcd    *etcd.Config    `yaml:"Etcd"`
	Redis   *redis.Config   `yaml:"Redis"`
	Sql     *sql.Config     `yaml:"Sql"`
	Service *service.Config `yaml:"Service"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	if c == nil {
		hlog.Error("not found app config")
		return false
	}

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
	if !c.Sql.Validate() {
		valid = false
	}
	if !c.Service.Validate() {
		valid = false
	}
	return valid
}

// Equal 判断两个Config是否相同
func (c *Config) Equal(other *Config) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}
	return c.Nats.Equal(other.Nats) &&
		c.Etcd.Equal(other.Etcd) &&
		c.Redis.Equal(other.Redis) &&
		c.Sql.Equal(other.Sql) &&
		c.Service.Equal(other.Service)

}
