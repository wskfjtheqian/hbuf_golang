package nats

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"time"
)

type Config struct {
	User             string        `yaml:"user"`
	Password         string        `yaml:"password"`
	Name             string        `yaml:"name"`
	ReconnectBufSize int           `yaml:"reconnectBufSize"`
	MaxReconnects    int           `yaml:"maxReconnects"`
	Timeout          time.Duration `yaml:"timeout"`
	PingInterval     time.Duration `yaml:"pingInterval"`
	Addrs            []string      `yaml:"addrs"`
	AckWait          time.Duration `yaml:"ackWait"`
	MaxDeliver       int           `yaml:"maxDeliver"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	var valid bool = true
	if c.User == "" {
		hlog.Error("User field is required")
		valid = false
	}
	if c.Password == "" {
		hlog.Error("Password field is required")
		valid = false
	}
	if c.Name == "" {
		hlog.Error("Name field is required")
		valid = false
	}
	if c.ReconnectBufSize < 0 {
		hlog.Error("ReconnectBufSize field is required")
		valid = false
	}
	if c.MaxReconnects < 0 {
		hlog.Error("MaxReconnects field is required")
		valid = false
	}
	if c.Timeout < 0 {
		hlog.Error("Timeout field is required")
		valid = false
	}
	if c.PingInterval < 0 {
		hlog.Error("PingInterval field is required")
		valid = false
	}
	if len(c.Addrs) == 0 {
		hlog.Error("Addrs field is required")
		valid = false
	}
	if c.AckWait < 0 {
		hlog.Error("AckWait field is required")
		valid = false
	}
	if c.MaxDeliver < 0 {
		hlog.Error("MaxDeliver field is required")
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
	if c.User != other.User ||
		c.Password != other.Password ||
		c.Name != other.Name ||
		c.ReconnectBufSize != other.ReconnectBufSize ||
		c.MaxReconnects != other.MaxReconnects ||
		c.Timeout != other.Timeout ||
		c.PingInterval != other.PingInterval ||
		len(c.Addrs) != len(other.Addrs) ||
		c.AckWait != other.AckWait ||
		c.MaxDeliver != other.MaxDeliver {
		return false
	}
	for i := 0; i < len(c.Addrs); i++ {
		if c.Addrs[i] != other.Addrs[i] {
			return false
		}
	}
	return true
}
