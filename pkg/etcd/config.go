package etcd

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/utl"
	"sort"
	"time"
)

// Config 配置
type Config struct {
	// Endpoints is a list of URLs.
	Endpoints []string `yaml:"Endpoints"`

	// AutoSyncInterval is the interval to update endpoints with its latest members.
	// 0 disables auto-sync. By default auto-sync is disabled.
	AutoSyncInterval *time.Duration `yaml:"AutoSyncInterval"`

	// DialTimeout is the timeout for failing to establish a connection.
	DialTimeout *time.Duration `yaml:"DialTimeout"`

	// DialKeepAliveTime is the time after which client pings the server to see if
	// transport is alive.
	DialKeepAliveTime *time.Duration `yaml:"DialKeepAliveTime"`

	// DialKeepAliveTimeout is the time that the client waits for a response for the
	// keep-alive probe. If the response is not received in this time, the connection is closed.
	DialKeepAliveTimeout *time.Duration `yaml:"DialKeepAliveTimeout"`

	// MaxCallSendMsgSize is the client-side request send limit in bytes.
	// If 0, it defaults to 2.0 MiB (2 * 1024 * 1024).
	// Make sure that "MaxCallSendMsgSize" < server-side default send/recv limit.
	// ("--max-request-bytes" flag to etcd or "embed.Config.MaxRequestBytes").
	MaxCallSendMsgSize *int `yaml:"MaxCallSendMsgSize"`

	// MaxCallRecvMsgSize is the client-side response receive limit.
	// If 0, 8it defaults to "math.MaxInt32", because range response can
	// easily exceed request send limits.
	// Make sure that "MaxCallRecvMsgSize" >= server-side default send/recv limit.
	// ("--max-request-bytes" flag to etcd or "embed.Config.MaxRequestBytes").
	MaxCallRecvMsgSize *int `yaml:"MaxCallRecvMsgSize"`

	// Username is a user name for authentication.
	Username *string `yaml:"Username"`

	// Password is a password for authentication.
	Password *string `yaml:"Password"`

	// RejectOldCluster when set will refuse to create a client against an outdated cluster.
	RejectOldCluster *bool `yaml:"RejectOldCluster"`

	// PermitWithoutStream when set will allow client to send keepalive pings to server without any active streams(RPCs).
	PermitWithoutStream *bool `yaml:"PermitWithoutStream"`

	// MaxUnaryRetries is the maximum number of retries for unary RPCs.
	MaxUnaryRetries *uint `yaml:"MaxUnaryRetries"`

	// BackoffWaitBetween is the wait time before retrying an RPC.
	BackoffWaitBetween *time.Duration `yaml:"BackoffWaitBetween"`

	// BackoffJitterFraction is the jitter fraction to randomize backoff wait time.
	BackoffJitterFraction *float64 `yaml:"BackoffJitterFraction"`
}

// Validate 验证配置是否有效
func (c *Config) Validate() bool {
	valid := true

	if c.Endpoints == nil || len(c.Endpoints) == 0 {
		valid = false
		hlog.Error("etcd endpoints must not be empty")
	}
	for _, ep := range c.Endpoints {
		if ep == "" {
			valid = false
			hlog.Error("etcd endpoint must not be empty")
		}
	}
	if c.AutoSyncInterval != nil && *c.AutoSyncInterval <= 0 {
		valid = false
		hlog.Error("etcd auto-sync interval must be greater than or equal to 0")
	}
	if c.DialTimeout != nil && *c.DialTimeout <= 0 {
		valid = false
		hlog.Error("etcd dial timeout must be greater than or equal to 0")
	}
	if c.DialKeepAliveTime != nil && *c.DialKeepAliveTime <= 0 {
		valid = false
		hlog.Error("etcd dial keep-alive time must be greater than or equal to 0")
	}
	if c.DialKeepAliveTimeout != nil && *c.DialKeepAliveTimeout <= 0 {
		valid = false
		hlog.Error("etcd dial keep-alive timeout must be greater than or equal to 0")
	}
	if c.MaxCallSendMsgSize != nil && *c.MaxCallSendMsgSize < 0 {
		valid = false
		hlog.Error("etcd max call send message size must be greater than or equal to 0")
	}
	if c.MaxCallRecvMsgSize != nil && *c.MaxCallRecvMsgSize < 0 {
		valid = false
		hlog.Error("etcd max call receive message size must be greater than or equal to 0")
	}
	if c.MaxUnaryRetries != nil && *c.MaxUnaryRetries < 0 {
		valid = false
		hlog.Error("etcd max unary retries must be greater than or equal to 0")
	}
	if c.BackoffWaitBetween != nil && *c.BackoffWaitBetween < 0 {
		valid = false
		hlog.Error("etcd backoff wait between must be greater than or equal to 0")
	}
	if c.BackoffJitterFraction != nil && *c.BackoffJitterFraction < 0 {
		valid = false
		hlog.Error("etcd backoff jitter fraction must be greater than or equal to 0")
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
	if len(c.Endpoints) > 0 {
		sort.Strings(c.Endpoints)
		sort.Strings(other.Endpoints)
	}
	for i := range c.Endpoints {
		if c.Endpoints[i] != other.Endpoints[i] {
			return false
		}
	}
	return utl.Equal(c.AutoSyncInterval, other.AutoSyncInterval) &&
		utl.Equal(c.DialTimeout, other.DialTimeout) &&
		utl.Equal(c.DialKeepAliveTime, other.DialKeepAliveTime) &&
		utl.Equal(c.DialKeepAliveTimeout, other.DialKeepAliveTimeout) &&
		utl.Equal(c.MaxCallSendMsgSize, other.MaxCallSendMsgSize) &&
		utl.Equal(c.MaxCallRecvMsgSize, other.MaxCallRecvMsgSize) &&
		utl.Equal(c.Username, other.Username) &&
		utl.Equal(c.Password, other.Password) &&
		utl.Equal(c.RejectOldCluster, other.RejectOldCluster) &&
		utl.Equal(c.PermitWithoutStream, other.PermitWithoutStream) &&
		utl.Equal(c.MaxUnaryRetries, other.MaxUnaryRetries) &&
		utl.Equal(c.BackoffWaitBetween, other.BackoffWaitBetween) &&
		utl.Equal(c.BackoffJitterFraction, other.BackoffJitterFraction)
}
