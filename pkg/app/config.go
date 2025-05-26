package app

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/nats"
	"github.com/wskfjtheqian/hbuf_golang/pkg/redis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/sql"
)

type Config struct {
	Nats  *nats.Config  `yaml:"Nats"`
	Etcd  *etcd.Config  `yaml:"Etcd"`
	Redis *redis.Config `yaml:"Redis"`
	Sql   *sql.Config   `yaml:"Sql"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	valid := true
	if c.Nats == nil || !c.Nats.Validate() {
		valid = false
		hlog.Error("nats config is invalid")
	}
	if c.Etcd == nil || !c.Etcd.Validate() {
		valid = false
		hlog.Error("etcd config is invalid")
	}
	if c.Redis == nil || !c.Redis.Validate() {
		valid = false
		hlog.Error("redis config is invalid")
	}
	if c.Sql == nil || !c.Sql.Validate() {
		valid = false
		hlog.Error("sql config is invalid")
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
		c.Sql.Equal(other.Sql)

}
