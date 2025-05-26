package sql

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/utl"
	"time"
)

// Config 数据库配置
type Config struct {
	// SetMaxIdleConns sets the maximum number of connections in the idle
	// connection pool.
	//
	// If MaxOpenConns is greater than 0 but less than the new MaxIdleConns,
	// then the new MaxIdleConns will be reduced to match the MaxOpenConns limit.
	//
	// If n <= 0, no idle connections are retained.
	//
	// The default max idle connections is currently 2. This may change in
	// a future release.
	MaxOpenConns *int `yaml:"maxOpenConns"`

	// SetMaxIdleConns sets the maximum number of connections in the idle
	// connection pool.
	//
	// If MaxOpenConns is greater than 0 but less than the new MaxIdleConns,
	// then the new MaxIdleConns will be reduced to match the MaxOpenConns limit.
	//
	// If n <= 0, no idle connections are retained.
	//
	// The default max idle connections is currently 2. This may change in
	// a future release.
	MaxIdleConns *int `yaml:"maxIdleConns"`

	// SetConnMaxLifetime sets the maximum amount of time a connection may be reused.
	//
	// Expired connections may be closed lazily before reuse.
	//
	// If d <= 0, connections are not closed due to a connection's age.
	ConnMaxLifetime *time.Duration `yaml:"connMaxLifetime"`

	// SetConnMaxIdleTime sets the maximum amount of time a connection may be idle.
	//
	// Expired connections may be closed lazily before reuse.
	//
	// If d <= 0, connections are not closed due to a connection's idle time.
	ConnMaxIdleTime *time.Duration `yaml:"connMaxIdleTime"`

	// SetType sets the type of database.
	//
	// Currently supported types are:
	// - mysql
	Type *string `yaml:"type"`

	// SetUsername sets the username for the database.
	Username *string `yaml:"username"`

	// SetPassword sets the password for the database.
	Password *string `yaml:"password"`

	// SetURL sets the url for the database.
	URL *string `yaml:"url"`
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
