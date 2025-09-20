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
	MaxOpenConns *int `yaml:"MaxOpenConns"`

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
	MaxIdleConns *int `yaml:"MaxIdleConns"`

	// SetConnMaxLifetime sets the maximum amount of time a connection may be reused.
	//
	// Expired connections may be closed lazily before reuse.
	//
	// If d <= 0, connections are not closed due to a connection's age.
	ConnMaxLifetime *time.Duration `yaml:"ConnMaxLifetime"`

	// SetConnMaxIdleTime sets the maximum amount of time a connection may be idle.
	//
	// Expired connections may be closed lazily before reuse.
	//
	// If d <= 0, connections are not closed due to a connection's idle time.
	ConnMaxIdleTime *time.Duration `yaml:"ConnMaxIdleTime"`

	// SetType sets the type of database.
	//
	// Currently supported types are:
	// - mysql
	Type *string `yaml:"Type"`

	// SetUsername sets the username for the database.
	Username *string `yaml:"Username"`

	// SetPassword sets the password for the database.
	Password *string `yaml:"Password"`

	// SetDbName sets the name of the database.
	DbName *string `yaml:"DbName"`

	// SetCharset sets the charset for the database.
	Network *string `yaml:"Network"`

	// SetHost sets the host for the database.
	Host *string `yaml:"Host"`

	// SetPort sets the port for the database.
	Params *string `yaml:"Params"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	if c == nil {
		hlog.Error("not found database config")
		return false
	}

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
	if c.DbName == nil || *c.DbName == "" {
		valid = false
		hlog.Error("sql config dbname is empty")
	}
	if c.Network == nil || *c.Network == "" {
		valid = false
		hlog.Error("sql config network is empty")
	}
	if c.Host == nil || *c.Host == "" {
		valid = false
		hlog.Error("sql config host is empty")
	}
	if c.Params == nil {
		c.Params = utl.ToPointer("")
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
		utl.Equal(c.DbName, other.DbName) &&
		utl.Equal(c.Network, other.Network) &&
		utl.Equal(c.Host, other.Host) &&
		utl.Equal(c.Params, other.Params)
}
