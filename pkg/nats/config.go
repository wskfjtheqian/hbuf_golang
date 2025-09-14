package nats

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/utl"
	"sort"
	"time"
)

type Config struct {
	// Servers is a configured set of servers which this client
	// will use when attempting to connect.
	Servers []string `yaml:"Servers"`

	// NoRandomize configures whether we will randomize the
	// server pool.
	NoRandomize *bool `yaml:"NoRandomize"`

	// NoEcho configures whether the server will echo back messages
	// that are sent on this connection if we also have matching subscriptions.
	// Note this is supported on servers >= version 1.2. Proto 1 or greater.
	NoEcho *bool `yaml:"NoEcho"`

	// Name is an optional name label which will be sent to the server
	// on CONNECT to identify the client.
	Name *string `yaml:"Name"`

	// Verbose signals the server to send an OK ack for commands
	// successfully processed by the server.
	Verbose *bool `yaml:"Verbose"`

	// Secure enables TLS secure connections that skip server
	// verification by default. NOT RECOMMENDED.
	Secure *bool `yaml:"Secure"`

	// AllowReconnect enables reconnection logic to be used when we
	// encounter a disconnect from the current server.
	AllowReconnect *bool `yaml:"AllowReconnect"`

	// MaxReconnect sets the number of reconnect attempts that will be
	// tried before giving up. If negative, then it will never give up
	// trying to reconnect.
	// Defaults to 60.
	MaxReconnect *int `yaml:"MaxReconnect"`

	// ReconnectWait sets the time to backoff after attempting a reconnect
	// to a server that we were already connected to previously.
	// Defaults to 2s.
	ReconnectWait *time.Duration `yaml:"ReconnectWait"`

	// ReconnectJitter sets the upper bound for a random delay added to
	// ReconnectWait during a reconnect when no TLS is used.
	// Defaults to 100ms.
	ReconnectJitter *time.Duration `yaml:"ReconnectJitter"`

	// ReconnectJitterTLS sets the upper bound for a random delay added to
	// ReconnectWait during a reconnect when TLS is used.
	// Defaults to 1s.
	ReconnectJitterTLS *time.Duration `yaml:"ReconnectJitterTLS"`

	// Timeout sets the timeout for a Dial operation on a connection.
	// Defaults to 2s.
	Timeout *time.Duration `yaml:"Timeout"`

	// DrainTimeout sets the timeout for a Drain Operation to complete.
	// Defaults to 30s.
	DrainTimeout *time.Duration `yaml:"DrainTimeout"`

	// FlusherTimeout is the maximum time to wait for write operations
	// to the underlying connection to complete (including the flusher loop).
	// Defaults to 1m.
	FlusherTimeout *time.Duration `yaml:"FlusherTimeout"`

	// PingInterval is the period at which the client will be sending ping
	// commands to the server, disabled if 0 or negative.
	// Defaults to 2m.
	PingInterval *time.Duration `yaml:"PingInterval"`

	// MaxPingsOut is the maximum number of pending ping commands that can
	// be awaiting a response before raising an ErrStaleConnection error.
	// Defaults to 2.
	MaxPingsOut *int `yaml:"MaxPingsOut"`

	// ReconnectBufSize is the size of the backing bufio during reconnect.
	// Once this has been exhausted publish operations will return an error.
	// Defaults to 8388608 bytes (8MB).
	ReconnectBufSize *int `yaml:"ReconnectBufSize"`

	// SubChanLen is the size of the buffered channel used between the socket
	// Go routine and the message delivery for SyncSubscriptions.
	// NOTE: This does not affect AsyncSubscriptions which are
	// dictated by PendingLimits()
	// Defaults to 65536.
	SubChanLen *int `yaml:"SubChanLen"`

	// User sets the username to be used when connecting to the server.
	User *string `yaml:"User"`

	// Password sets the password to be used when connecting to a server.
	Password *string `yaml:"Password"`

	// Token sets the token to be used when connecting to a server.
	Token *string `yaml:"Token"`

	// UseOldRequestStyle forces the old method of Requests that utilize
	// a new Inbox and a new Subscription for each request.
	UseOldRequestStyle *bool `yaml:"UseOldRequestStyle"`

	// NoCallbacksAfterClientClose allows preventing the invocation of
	// callbacks after Close() is called. Client won't receive notifications
	// when Close is invoked by user code. Default is to invoke the callbacks.
	NoCallbacksAfterClientClose *bool `yaml:"NoCallbacksAfterClientClose"`

	// RetryOnFailedConnect sets the connection in reconnecting state right
	// away if it can't connect to a server in the initial set. The
	// MaxReconnect and ReconnectWait options are used for this process,
	// similarly to when an established connection is disconnected.
	// If a ReconnectHandler is set, it will be invoked on the first
	// successful reconnect attempt (if the initial connect fails),
	// and if a ClosedHandler is set, it will be invoked if
	// it fails to connect (after exhausting the MaxReconnect attempts).
	RetryOnFailedConnect *bool `yaml:"RetryOnFailedConnect"`

	// For websocket connections, indicates to the server that the connection
	// supports compression. If the server does too, then data will be compressed.
	Compression *bool `yaml:"Compression"`

	// For websocket connections, adds a path to connections url.
	// This is useful when connecting to NATS behind a proxy.
	ProxyPath *string `yaml:"ProxyPath"`

	// InboxPrefix allows the default _INBOX prefix to be customized
	InboxPrefix *string `yaml:"InboxPrefix"`

	// IgnoreAuthErrorAbort - if set to true, client opts out of the default connect behavior of aborting
	// subsequent reconnect attempts if server returns the same auth error twice (regardless of reconnect policy).
	IgnoreAuthErrorAbort *bool `yaml:"IgnoreAuthErrorAbort"`

	// SkipHostLookup skips the DNS lookup for the server hostname.
	SkipHostLookup *bool `yaml:"SkipHostLookup"`

	// PermissionErrOnSubscribe - if set to true, the client will return ErrPermissionViolation
	// from SubscribeSync if the server returns a permissions error for a subscription.
	// Defaults to false.
	PermissionErrOnSubscribe *bool `yaml:"PermissionErrOnSubscribe"`
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	if c == nil {
		hlog.Error("not found nats config")
		return false
	}

	var valid bool = true
	if c.Servers != nil && len(c.Servers) < 0 {
		valid = false
		hlog.Error("Servers is not allowed in Config")
	}
	for _, s := range c.Servers {
		if len(s) == 0 {
			valid = false
			hlog.Error("Empty server in Config")
		}
	}
	if c.MaxReconnect != nil && *c.MaxReconnect < 0 {
		valid = false
		hlog.Error("MaxReconnect must be positive in Config")
	}
	if c.ReconnectWait != nil && *c.ReconnectWait < 0 {
		valid = false
		hlog.Error("ReconnectWait must be positive in Config")
	}
	if c.ReconnectJitter != nil && *c.ReconnectJitter < 0 {
		valid = false
		hlog.Error("ReconnectJitter must be positive in Config")
	}
	if c.ReconnectJitterTLS != nil && *c.ReconnectJitterTLS < 0 {
		valid = false
		hlog.Error("ReconnectJitterTLS must be positive in Config")
	}
	if c.Timeout != nil && *c.Timeout < 0 {
		valid = false
		hlog.Error("Timeout must be positive in Config")
	}
	if c.DrainTimeout != nil && *c.DrainTimeout < 0 {
		valid = false
		hlog.Error("DrainTimeout must be positive in Config")
	}
	if c.FlusherTimeout != nil && *c.FlusherTimeout < 0 {
		valid = false
		hlog.Error("FlusherTimeout must be positive in Config")
	}
	if c.PingInterval != nil && *c.PingInterval < 0 {
		valid = false
		hlog.Error("PingInterval must be positive in Config")
	}
	if c.MaxPingsOut != nil && *c.MaxPingsOut < 0 {
		valid = false
		hlog.Error("MaxPingsOut must be positive in Config")
	}
	if c.ReconnectBufSize != nil && *c.ReconnectBufSize < 0 {
		valid = false
		hlog.Error("ReconnectBufSize must be positive in Config")
	}
	if c.SubChanLen != nil && *c.SubChanLen < 0 {
		valid = false
		hlog.Error("SubChanLen must be positive in Config")
	}
	if c.User != nil && len(*c.User) < 0 {
		valid = false
		hlog.Error("User is not allowed in Config")
	}
	if c.Password != nil && len(*c.Password) < 0 {
		valid = false
		hlog.Error("Password is not allowed in Config")
	}
	if c.Token != nil && len(*c.Token) < 0 {
		valid = false
		hlog.Error("Token is not allowed in Config")
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

	if len(c.Servers) != len(other.Servers) {
		return false
	}
	if len(c.Servers) > 0 {
		sort.Strings(c.Servers)
		sort.Strings(other.Servers)
	}
	for i := range c.Servers {
		if c.Servers[i] != other.Servers[i] {
			return false
		}
	}
	return utl.Equal(c.NoRandomize, other.NoRandomize) &&
		utl.Equal(c.NoEcho, other.NoEcho) &&
		utl.Equal(c.Name, other.Name) &&
		utl.Equal(c.Verbose, other.Verbose) &&
		utl.Equal(c.Secure, other.Secure) &&
		utl.Equal(c.AllowReconnect, other.AllowReconnect) &&
		utl.Equal(c.MaxReconnect, other.MaxReconnect) &&
		utl.Equal(c.ReconnectWait, other.ReconnectWait) &&
		utl.Equal(c.ReconnectJitter, other.ReconnectJitter) &&
		utl.Equal(c.ReconnectJitterTLS, other.ReconnectJitterTLS) &&
		utl.Equal(c.Timeout, other.Timeout) &&
		utl.Equal(c.DrainTimeout, other.DrainTimeout) &&
		utl.Equal(c.FlusherTimeout, other.FlusherTimeout) &&
		utl.Equal(c.PingInterval, other.PingInterval) &&
		utl.Equal(c.MaxPingsOut, other.MaxPingsOut) &&
		utl.Equal(c.ReconnectBufSize, other.ReconnectBufSize) &&
		utl.Equal(c.SubChanLen, other.SubChanLen) &&
		utl.Equal(c.User, other.User) &&
		utl.Equal(c.Password, other.Password) &&
		utl.Equal(c.Token, other.Token) &&
		utl.Equal(c.UseOldRequestStyle, other.UseOldRequestStyle) &&
		utl.Equal(c.NoCallbacksAfterClientClose, other.NoCallbacksAfterClientClose) &&
		utl.Equal(c.RetryOnFailedConnect, other.RetryOnFailedConnect) &&
		utl.Equal(c.Compression, other.Compression) &&
		utl.Equal(c.ProxyPath, other.ProxyPath) &&
		utl.Equal(c.InboxPrefix, other.InboxPrefix) &&
		utl.Equal(c.IgnoreAuthErrorAbort, other.IgnoreAuthErrorAbort) &&
		utl.Equal(c.SkipHostLookup, other.SkipHostLookup) &&
		utl.Equal(c.PermissionErrOnSubscribe, other.PermissionErrOnSubscribe)
}
