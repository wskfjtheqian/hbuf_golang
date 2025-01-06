package redis

import "time"

type Config struct {
	Password        string        `yaml:"password"`
	DB              int           `yaml:"db"`
	PoolTimeout     time.Duration `yaml:"poolTimeout"`
	MaxConnAge      time.Duration `yaml:"maxConnAge"`
	MinIdleConns    int           `yaml:"minIdleConns"`
	PoolSize        int           `yaml:"poolSize"`
	WriteTimeout    time.Duration `yaml:"writeTimeout"`
	ReadTimeout     time.Duration `yaml:"readTimeout"`
	DialTimeout     time.Duration `yaml:"dialTimeout"`
	MaxRetryBackoff time.Duration `yaml:"maxRetryBackoff"`
	MinRetryBackoff time.Duration `yaml:"minRetryBackoff"`
	MaxRetries      int           `yaml:"maxRetries"`
	Addr            string        `yaml:"addr"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	var valid bool = true
	if c.DB < 0 {
		valid = false
	}
	if c.PoolTimeout < 0 {
		valid = false
	}
	if c.MaxConnAge < 0 {
		valid = false
	}
	if c.MinIdleConns < 0 {
		valid = false
	}
	if c.PoolSize < 0 {
		valid = false
	}
	if c.WriteTimeout < 0 {
		valid = false
	}
	if c.ReadTimeout < 0 {
		valid = false
	}
	if c.DialTimeout < 0 {
		valid = false
	}
	if c.MaxRetryBackoff < 0 {
		valid = false
	}
	if c.MinRetryBackoff < 0 {
		valid = false
	}
	if c.MaxRetries < 0 {
		valid = false
	}
	if c.Addr == "" {
		valid = false
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

	return c.Password == other.Password &&
		c.DB == other.DB &&
		c.PoolTimeout == other.PoolTimeout &&
		c.MaxConnAge == other.MaxConnAge &&
		c.MinIdleConns == other.MinIdleConns &&
		c.PoolSize == other.PoolSize &&
		c.WriteTimeout == other.WriteTimeout &&
		c.ReadTimeout == other.ReadTimeout &&
		c.DialTimeout == other.DialTimeout &&
		c.MaxRetryBackoff == other.MaxRetryBackoff &&
		c.MinRetryBackoff == other.MinRetryBackoff &&
		c.MaxRetries == other.MaxRetries &&
		c.Addr == other.Addr
}
