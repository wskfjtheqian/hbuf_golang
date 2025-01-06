package redis

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/utl"
	"time"
)

// Config redis配置
type Config struct {
	Password        *string        `yaml:"password"`
	DB              *int           `yaml:"db"`
	Addr            *string        `yaml:"addr"`
	PoolTimeout     *time.Duration `yaml:"poolTimeout"`
	MaxConnAge      *time.Duration `yaml:"maxConnAge"`
	MinIdleConns    *int           `yaml:"minIdleConns"`
	PoolSize        *int           `yaml:"poolSize"`
	WriteTimeout    *time.Duration `yaml:"writeTimeout"`
	ReadTimeout     *time.Duration `yaml:"readTimeout"`
	DialTimeout     *time.Duration `yaml:"dialTimeout"`
	MaxRetryBackoff *time.Duration `yaml:"maxRetryBackoff"`
	MinRetryBackoff *time.Duration `yaml:"minRetryBackoff"`
	MaxRetries      *int           `yaml:"maxRetries"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	var valid bool = true
	if c.Password == nil || *c.Password == "" {
		valid = false
		hlog.Error("redis password is empty")
	}
	if c.DB == nil || *c.DB < 0 {
		valid = false
		hlog.Error("redis db is invalid")
	}
	if c.Addr == nil || *c.Addr == "" {
		valid = false
		hlog.Error("redis addr is empty")
	}
	return valid
}

// Equal 判断两个配置是否相同
func (c *Config) Equal(other *Config) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}

	return utl.Equal(c.Password, other.Password) &&
		utl.Equal(c.DB, other.DB) &&
		utl.Equal(c.Addr, other.Addr) &&
		utl.Equal(c.PoolTimeout, other.PoolTimeout) &&
		utl.Equal(c.MaxConnAge, other.MaxConnAge) &&
		utl.Equal(c.MinIdleConns, other.MinIdleConns) &&
		utl.Equal(c.PoolSize, other.PoolSize) &&
		utl.Equal(c.WriteTimeout, other.WriteTimeout) &&
		utl.Equal(c.ReadTimeout, other.ReadTimeout) &&
		utl.Equal(c.DialTimeout, other.DialTimeout) &&
		utl.Equal(c.MaxRetryBackoff, other.MaxRetryBackoff) &&
		utl.Equal(c.MinRetryBackoff, other.MinRetryBackoff) &&
		utl.Equal(c.MaxRetries, other.MaxRetries)
}
