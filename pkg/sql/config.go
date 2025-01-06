package sql

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/utl"
	"time"
)

// Config 数据库配置
type Config struct {
	MaxOpenConns    *int           `yaml:"maxOpenConns"`
	MaxIdleConns    *int           `yaml:"maxIdleConns"`
	ConnMaxLifetime *time.Duration `yaml:"connMaxLifetime"`
	ConnMaxIdleTime *time.Duration `yaml:"connMaxIdleTime"`
	Type            *string        `yaml:"type"`
	Username        *string        `yaml:"username"`
	Password        *string        `yaml:"password"`
	URL             *string        `yaml:"url"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	var valid bool = true
	if c.Type == nil || *c.Type == "" {
		valid = false
		hlog.Error("sql config type is empty")
	}
	if c.Username == nil || *c.Username == "" {
		valid = false
		hlog.Error("sql config username is empty")
	}
	if c.Password == nil || *c.Password == "" {
		valid = false
		hlog.Error("sql config password is empty")
	}
	if c.URL == nil || *c.URL == "" {
		valid = false
		hlog.Error("sql config url is empty")
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

	return utl.Equal(c.MaxOpenConns, other.MaxOpenConns) &&
		utl.Equal(c.MaxIdleConns, other.MaxIdleConns) &&
		utl.Equal(c.ConnMaxLifetime, other.ConnMaxLifetime) &&
		utl.Equal(c.ConnMaxIdleTime, other.ConnMaxIdleTime) &&
		utl.Equal(c.Type, other.Type) &&
		utl.Equal(c.Username, other.Username) &&
		utl.Equal(c.Password, other.Password) &&
		utl.Equal(c.URL, other.URL)
}
