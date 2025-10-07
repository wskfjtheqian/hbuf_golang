package hredis

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"time"
)

// Config redis配置
type Config struct {
	// The network type, either tcp or unix.
	// Default is tcp.
	Network *string `yaml:"Network"`
	// host:port address.
	Addr *string `yaml:"Addr"`
	// Use the specified Username to authenticate the current connection
	// with one of the connections defined in the ACL list when connecting
	// to a Redis 6.0 instance, or greater, that is using the Redis ACL system.
	Username *string `yaml:"Username"`
	// Optional password. Must match the password specified in the
	// requirepass server configuration option (if connecting to a Redis 5.0 instance, or lower),
	// or the User Password when connecting to a Redis 6.0 instance, or greater,
	// that is using the Redis ACL system.
	Password *string `yaml:"Password"`

	// Database to be selected after connecting to the server.
	DB *int `yaml:"DB"`

	// Maximum number of retries before giving up.
	// Default is 3 retries; -1 (not 0) disables retries.
	MaxRetries *int `yaml:"MaxRetries"`
	// Minimum backoff between each retry.
	// Default is 8 milliseconds; -1 disables backoff.
	MinRetryBackoff *time.Duration `yaml:"MinRetryBackoff"`
	// Maximum backoff between each retry.
	// Default is 512 milliseconds; -1 disables backoff.
	MaxRetryBackoff *time.Duration `yaml:"MaxRetryBackoff"`

	// Dial timeout for establishing new connections.
	// Default is 5 seconds.
	DialTimeout *time.Duration `yaml:"DialTimeout"`
	// Timeout for socket reads. If reached, commands will fail
	// with a timeout instead of blocking. Use value -1 for no timeout and 0 for default.
	// Default is 3 seconds.
	ReadTimeout *time.Duration `yaml:"ReadTimeout"`
	// Timeout for socket writes. If reached, commands will fail
	// with a timeout instead of blocking.
	// Default is ReadTimeout.
	WriteTimeout *time.Duration `yaml:"WriteTimeout"`

	// Type of connection pool.
	// true for FIFO pool, false for LIFO pool.
	// Note that fifo has higher overhead compared to lifo.
	PoolFIFO *bool `yaml:"PoolFIFO"`
	// Maximum number of socket connections.
	// Default is 10 connections per every available CPU as reported by runtime.GOMAXPROCS.
	PoolSize *int `yaml:"PoolSize"`
	// Minimum number of idle connections which is useful when establishing
	// new connection is slow.
	MinIdleConns *int `yaml:"MinIdleConns"`
	// Connection age at which client retires (closes) the connection.
	// Default is to not close aged connections.
	MaxConnAge *time.Duration `yaml:"MaxConnAge"`
	// Amount of time client waits for connection if all connections
	// are busy before returning an error.
	// Default is ReadTimeout + 1 second.
	PoolTimeout *time.Duration `yaml:"PoolTimeout"`
	// Amount of time after which client closes idle connections.
	// Should be less than server's timeout.
	// Default is 5 minutes. -1 disables idle timeout check.
	IdleTimeout *time.Duration `yaml:"IdleTimeout"`
	// Frequency of idle checks made by idle connections reaper.
	// Default is 1 minute. -1 disables idle connections reaper,
	// but idle connections are still discarded by the client
	// if IdleTimeout is set.
	IdleCheckFrequency *time.Duration `yaml:"IdleCheckFrequency"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	if c == nil {
		hlog.Error("not found redis config")
		return false
	}

	var valid bool = true
	if c.Network != nil && *c.Network != "tcp" && *c.Network != "unix" {
		valid = false
		hlog.Error("redis network is invalid")
	}
	if c.Addr == nil || *c.Addr == "" {
		valid = false
		hlog.Error("redis addr is invalid")
	}
	if c.DB != nil && *c.DB < 0 {
		valid = false
		hlog.Error("redis db is invalid")
	}
	if c.MaxRetries != nil && *c.MaxRetries < -1 {
		valid = false
		hlog.Error("redis max retries is invalid")
	}
	if c.MinRetryBackoff != nil && *c.MinRetryBackoff < 0 {
		valid = false
		hlog.Error("redis min retry backoff is invalid")
	}
	if c.MaxRetryBackoff != nil && *c.MaxRetryBackoff < -1 {
		valid = false
		hlog.Error("redis max retry backoff is invalid")
	}
	if c.DialTimeout != nil && *c.DialTimeout < 0 {
		valid = false
		hlog.Error("redis dial timeout is invalid")
	}
	if c.ReadTimeout != nil && *c.ReadTimeout < -1 {
		valid = false
		hlog.Error("redis read timeout is invalid")
	}
	if c.WriteTimeout != nil && *c.WriteTimeout < 0 {
		valid = false
		hlog.Error("redis write timeout is invalid")
	}
	if c.PoolFIFO != nil && *c.PoolFIFO {
		valid = false
		hlog.Error("redis pool fifo is invalid")
	}
	if c.PoolSize != nil && *c.PoolSize < 0 {
		valid = false
		hlog.Error("redis pool size is invalid")
	}
	if c.MinIdleConns != nil && *c.MinIdleConns < 0 {
		valid = false
		hlog.Error("redis min idle conns is invalid")
	}
	if c.MaxConnAge != nil && *c.MaxConnAge < 0 {
		valid = false
		hlog.Error("redis max conn age is invalid")
	}
	if c.PoolTimeout != nil && *c.PoolTimeout < 0 {
		valid = false
		hlog.Error("redis pool timeout is invalid")
	}
	if c.IdleTimeout != nil && *c.IdleTimeout < -1 {
		valid = false
		hlog.Error("redis idle timeout is invalid")
	}
	if c.IdleCheckFrequency != nil && *c.IdleCheckFrequency < -1 {
		valid = false
		hlog.Error("redis idle check frequency is invalid")
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

	return hutl.Equal(c.Network, other.Network) &&
		hutl.Equal(c.Addr, other.Addr) &&
		hutl.Equal(c.Username, other.Username) &&
		hutl.Equal(c.Password, other.Password) &&
		hutl.Equal(c.DB, other.DB) &&
		hutl.Equal(c.MaxRetries, other.MaxRetries) &&
		hutl.Equal(c.MinRetryBackoff, other.MinRetryBackoff) &&
		hutl.Equal(c.MaxRetryBackoff, other.MaxRetryBackoff) &&
		hutl.Equal(c.DialTimeout, other.DialTimeout) &&
		hutl.Equal(c.ReadTimeout, other.ReadTimeout) &&
		hutl.Equal(c.WriteTimeout, other.WriteTimeout) &&
		hutl.Equal(c.PoolFIFO, other.PoolFIFO) &&
		hutl.Equal(c.PoolSize, other.PoolSize) &&
		hutl.Equal(c.MinIdleConns, other.MinIdleConns) &&
		hutl.Equal(c.MaxConnAge, other.MaxConnAge) &&
		hutl.Equal(c.PoolTimeout, other.PoolTimeout) &&
		hutl.Equal(c.IdleTimeout, other.IdleTimeout) &&
		hutl.Equal(c.IdleCheckFrequency, other.IdleCheckFrequency)
}
