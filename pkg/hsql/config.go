package hsql

import (
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

// Config 数据库配置
type Config struct {
	// SetMaxIdleConns 设置空闲连接池中的最大连接数
	//
	// 如果 MaxOpenConns 大于0但小于新的 MaxIdleConns，
	// 则新的 MaxIdleConns 将被减少以匹配 MaxOpenConns 限制
	//
	// 如果 n <= 0，则不保留空闲连接
	//
	// 默认最大空闲连接数当前为2，未来版本可能会更改
	MaxOpenConns *int `yaml:"MaxOpenConns"`

	// SetMaxIdleConns 设置空闲连接池中的最大连接数
	//
	// 如果 MaxOpenConns 大于0但小于新的 MaxIdleConns，
	// 则新的 MaxIdleConns 将被减少以匹配 MaxOpenConns 限制
	//
	// 如果 n <= 0，则不保留空闲连接
	//
	// 默认最大空闲连接数当前为2，未来版本可能会更改
	MaxIdleConns *int `yaml:"MaxIdleConns"`

	// SetConnMaxLifetime 设置连接可被重用的最长时间
	//
	// 过期的连接可能会在重用前被延迟关闭
	//
	// 如果 d <= 0，则不会因为连接的使用时间而关闭连接
	ConnMaxLifetime *time.Duration `yaml:"ConnMaxLifetime"`

	// SetConnMaxIdleTime 设置连接可处于空闲状态的最长时间
	//
	// 过期的连接可能会在重用前被延迟关闭
	//
	// 如果 d <= 0，则不会因为连接的空闲时间而关闭连接
	ConnMaxIdleTime *time.Duration `yaml:"ConnMaxIdleTime"`

	// SetType 设置数据库类型
	//
	// 当前支持的类型有：
	// - mysql
	Type *string `yaml:"Type"`

	// SetUsername 设置数据库用户名
	Username *string `yaml:"Username"`

	// SetPassword 设置数据库密码
	Password *string `yaml:"Password"`

	// SetDbName 设置数据库名称
	DbName *string `yaml:"DbName"`

	// SetCharset 设置数据库字符集
	Network *string `yaml:"Network"`

	// SetHost 设置数据库主机地址
	Host *string `yaml:"Host"`

	// SetPort 设置数据库端口
	Params *string `yaml:"Params"`

	//启用缓存
	EnableCache bool `yaml:"EnableCache"`

	//批量操作限制
	BatchLimit *uint `yaml:"BatchLimit"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	if c == nil {
		hlog.Error("未找到数据库配置")
		return false
	}

	var valid bool = true
	if c.Type == nil || *c.Type == "" {
		valid = false
		hlog.Error("数据库配置类型为空")
	}
	if c.Username == nil || *c.Username == "" {
		valid = false
		hlog.Error("数据库配置用户名为空")
	}
	if c.Password == nil || *c.Password == "" {
		valid = false
		hlog.Error("数据库配置密码为空")
	}
	if c.DbName == nil || *c.DbName == "" {
		valid = false
		hlog.Error("数据库配置数据库名称为空")
	}
	if c.Network == nil || *c.Network == "" {
		valid = false
		hlog.Error("数据库配置网络类型为空")
	}
	if c.Host == nil || *c.Host == "" {
		valid = false
		hlog.Error("数据库配置主机地址为空")
	}
	if c.Params == nil {
		c.Params = hutl.ToPointer("")
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

	return hutl.Equal(c.MaxOpenConns, other.MaxOpenConns) &&
		hutl.Equal(c.MaxIdleConns, other.MaxIdleConns) &&
		hutl.Equal(c.ConnMaxLifetime, other.ConnMaxLifetime) &&
		hutl.Equal(c.ConnMaxIdleTime, other.ConnMaxIdleTime) &&
		hutl.Equal(c.Type, other.Type) &&
		hutl.Equal(c.Username, other.Username) &&
		hutl.Equal(c.Password, other.Password) &&
		hutl.Equal(c.DbName, other.DbName) &&
		hutl.Equal(c.Network, other.Network) &&
		hutl.Equal(c.Host, other.Host) &&
		hutl.Equal(c.Params, other.Params) &&
		hutl.Equal(c.BatchLimit, other.BatchLimit) &&
		c.EnableCache == other.EnableCache
}
