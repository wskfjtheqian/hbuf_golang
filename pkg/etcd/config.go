package etcd

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"time"
)

// Config 配置
type Config struct {
	// AutoSyncInterval 本地数据与 etcd 同步的时间间隔
	AutoSyncInterval time.Duration `json:"auto-sync-interval"`

	// DialTimeout 在尝试建立连接时，如果连接失败的超时时间
	DialTimeout time.Duration `json:"dial-timeout"`

	// DialKeepAliveTime 户端在连接后，每隔多长时间向服务器发送一次心跳包以检测连接是否存活，
	DialKeepAliveTime time.Duration `json:"dial-keep-alive-time"`

	// DialKeepAliveTimeout 客户端在等待服务器响应时，最长等待时间
	DialKeepAliveTimeout time.Duration `json:"dial-keep-alive-timeout"`

	// Username 认证所需的用户名。
	Username string `json:"username"`

	// Password  认证所需的密码。
	Password string `json:"password"`

	// Endpoints  etcd 集群的连接地址
	Endpoints []string `json:"endpoints"`
}

// Validate 验证配置是否有效
func (c *Config) Validate() bool {
	valid := true

	if c.AutoSyncInterval <= 0 {
		valid = false
		hlog.Error("etcd auto-sync-interval must be greater than 0")
	}
	if c.DialTimeout <= 0 {
		valid = false
		hlog.Error("etcd dial-timeout must be greater than 0")
	}
	if c.DialKeepAliveTime <= 0 {
		valid = false
		hlog.Error("etcd dial-keep-alive-time must be greater than 0")
	}
	if c.DialKeepAliveTimeout <= 0 {
		valid = false
		hlog.Error("etcd dial-keep-alive-timeout must be greater than 0")
	}
	if len(c.Endpoints) == 0 {
		valid = false
		hlog.Error("etcd endpoints must not be empty")
	}
	if c.Username == "" {
		hlog.Warn("etcd username is empty")
	}
	if c.Password == "" {
		hlog.Warn("etcd password is empty")
	}
	for _, ep := range c.Endpoints {
		if ep == "" {
			valid = false
			hlog.Error("etcd endpoint must not be empty")
		}
	}
	return valid
}

// Equal 比较两个配置是否相同
func (c *Config) Equal(other *Config) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}
	if len(c.Endpoints) != len(other.Endpoints) {
		return false
	}
	for i := range c.Endpoints {
		if c.Endpoints[i] != other.Endpoints[i] {
			return false
		}
	}
	if c.DialTimeout != other.DialTimeout {
		return false
	}
	return true
}
