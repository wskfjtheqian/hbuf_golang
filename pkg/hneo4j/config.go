package hneo4j

import "github.com/wskfjtheqian/hbuf_golang/pkg/hlog"

type Config struct {
	Addr     *string `yaml:"Addr"`
	Username *string `yaml:"Username"`
	Password *string `yaml:"Password"`
	Database *string `yaml:"Database"`
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
		c.Database == other.Database
}
