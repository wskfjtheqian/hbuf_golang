package hneo4j

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"time"
)

// 默认配置常量（参考 NATS DefaultTimeout 等设计）
const (
	DefaultMaxConnectionPoolSize        = 100
	DefaultConnectionAcquisitionTimeout = 60   // 秒
	DefaultMaxConnectionLifetime        = 3600 // 秒, 1小时
	DefaultSocketConnectTimeout         = 10   // 秒
)

type Config struct {
	Addr     *string `yaml:"Addr"`
	Username *string `yaml:"Username"`
	Password *string `yaml:"Password"`
	Database *string `yaml:"Database"`

	// 连接池最大连接数，默认 100
	MaxConnectionPoolSize *int `yaml:"MaxConnectionPoolSize"`
	// 获取连接超时时间（秒），默认 60
	ConnectionAcquisitionTimeout *int `yaml:"ConnectionAcquisitionTimeout"`
	// 连接最大存活时间（秒），默认 3600（1小时），0 表示永不过期
	MaxConnectionLifetime *int `yaml:"MaxConnectionLifetime"`
	// Socket 连接超时（秒），默认 10
	SocketConnectTimeout *int `yaml:"SocketConnectTimeout"`
	// 是否启用 TCP KeepAlive，默认 true
	SocketKeepalive *bool `yaml:"SocketKeepalive"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	var valid = true
	if c == nil {
		hlog.Error("not found neo4j config")
		return false
	}
	if c.Addr == nil || *c.Addr == "" {
		valid = false
		hlog.Error("neo4j config Addr is empty")
	}
	if c.Username == nil || *c.Username == "" {
		valid = false
		hlog.Error("neo4j config username is empty")
	}
	if c.Password == nil || *c.Password == "" {
		valid = false
		hlog.Error("neo4j config password is empty")
	}
	if c.Database == nil || *c.Database == "" {
		valid = false
		hlog.Error("neo4j config database is empty")
	}
	return valid
}

func (c *Config) Equal(other *Config) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}

	return c.Addr == other.Addr &&
		c.Username == other.Username &&
		c.Password == other.Password &&
		c.Database == other.Database &&
		c.MaxConnectionPoolSize == other.MaxConnectionPoolSize &&
		c.ConnectionAcquisitionTimeout == other.ConnectionAcquisitionTimeout &&
		c.MaxConnectionLifetime == other.MaxConnectionLifetime &&
		c.SocketConnectTimeout == other.SocketConnectTimeout &&
		c.SocketKeepalive == other.SocketKeepalive
}

// durationVal 辅助函数：指针非nil且>0返回时间值，否则返回默认值
func durationVal(ptr *int, defaultSec int) time.Duration {
	if ptr != nil && *ptr > 0 {
		return time.Duration(*ptr) * time.Second
	}
	return time.Duration(defaultSec) * time.Second
}

// boolVal 辅助函数：指针非nil返回值，否则返回默认值
func boolVal(ptr *bool, defaultVal bool) bool {
	if ptr != nil {
		return *ptr
	}
	return defaultVal
}
