package hservice

import (
	"context"
	"crypto/tls"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

type Config struct {
	Server    *Server `yaml:"Server"`    //服务端配置
	Client    *Client `yaml:"Client"`    //客户端配置
	Codec     *string `yaml:"Codec"`     //编解码器: json | hbuf
	Transport *string `yaml:"Transport"` //传输层: http | h2
}

// 编解码器类型
const (
	CodecHBuf = "hbuf" // HBuf 二进制编解码
	CodecJSON = "json" // JSON 编解码
)

// 传输层类型
const (
	TransportHTTP     = "http" // 标准库 net/http 传输
	TransportH2Framer = "h2"   // hrpc.H2Framer 帧批处理传输
)

// GetCodec 获取编解码器类型，默认 json
func (c *Config) GetCodec() string {
	if c != nil && c.Codec != nil && *c.Codec == CodecHBuf {
		return CodecHBuf
	}
	return CodecJSON
}

// GetTransport 获取传输层类型，默认 http
func (c *Config) GetTransport() string {
	if c != nil && c.Transport != nil && *c.Transport == TransportH2Framer {
		return TransportH2Framer
	}
	return TransportHTTP
}

// Equal 判断两个Config是否相同
func (c *Config) Equal(other *Config) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}

	if c.Server.Equal(other.Server) && c.Client.Equal(other.Client) && c.Codec == other.Codec && c.Transport == other.Transport {
		return true
	}
	return false
}

// Validate 检查配置是否有效
func (c *Config) Validate(ctx context.Context) bool {
	if c == nil {
		hlog.Error(ctx, "not found server config")
		return false
	}

	var valid = true
	if c.Server == nil {
		valid = false
		hlog.Error(ctx, "server config is nil")
	}
	if c.Client == nil {
		valid = false
		hlog.Error(ctx, "client config is nil")
	}
	if c.Codec != nil && *c.Codec != CodecHBuf && *c.Codec != CodecJSON {
		valid = false
		hlog.Error(ctx, "unsupported codec: %s", *c.Codec)
	}
	if c.Transport != nil && *c.Transport != TransportHTTP && *c.Transport != TransportH2Framer {
		valid = false
		hlog.Error(ctx, "unsupported transport: %s", *c.Transport)
	}
	return valid
}

// Http 服务配置
type Http struct {
	Hostname *string `yaml:"Hostname"` //主机名
	Address  *string `yaml:"Address"`  //监听地址
	Crt      *string `yaml:"Crt"`      //crt证书
	Key      *string `yaml:"Key"`      //crt密钥
	Path     *string `yaml:"Path"`     //路径
}

// Validate 检查配置是否有效
func (h *Http) Validate(ctx context.Context) bool {
	var valid = true
	if h.Hostname == nil || *h.Hostname == "" {
		valid = false
		hlog.Error(ctx, "hostname is nil")
	}
	if h.Address == nil || *h.Address == "" {
		valid = false
		hlog.Error(ctx, "address is nil")
	}

	if h.Crt != nil && *h.Crt != "" && h.Key != nil && *h.Key != "" {
		_, err := tls.LoadX509KeyPair(*h.Crt, *h.Key)
		if err != nil {
			hlog.Error(ctx, "load x509 key pair error: %s", err.Error())
			valid = false
		}
	}

	if h.Path == nil {
		valid = false
		hlog.Error(ctx, "path is nil")
	}
	return valid
}

// Equal 判断两个Config是否相同
func (h *Http) Equal(other *Http) bool {
	if h == nil && other == nil {
		return true
	}
	if h == nil || other == nil {
		return false
	}
	if !(h.Hostname == other.Hostname && h.Address == other.Address && h.Crt == other.Crt && h.Key == other.Key && h.Path == other.Path) {
		return false
	}
	return true
}

// Server 服务配置
type Server struct {
	Register  bool     `yaml:"Register"`  //是否注册服务到注册中心
	Local     bool     `yaml:"Local"`     //是否开启本地服务
	Http      *Http    `yaml:"Http"`      //Http 服务配置
	List      []string `yaml:"List"`      //开始的服务列表
	LeaseTime int64    `yaml:"LeaseTime"` //租约时间
}

// Validate 检查配置是否有效
func (s *Server) Validate(ctx context.Context) bool {
	var valid = true
	if s.Http == nil {
		valid = false
		hlog.Error(ctx, "http config is nil")
	} else {
		valid = s.Http.Validate(ctx)
	}
	if s.List == nil {
		valid = false
		hlog.Error(ctx, "list is nil")
	}
	return valid
}

// Equal 判断两个Config是否相同
func (s *Server) Equal(other *Server) bool {
	if s == nil && other == nil {
		return true
	}
	if s == nil || other == nil {
		return false
	}
	if !(s.Register == other.Register && s.Local == other.Local && s.Http.Equal(other.Http) && len(s.List) == len(other.List)) {
		return false
	}
	for i, v := range s.List {
		if v != (other.List)[i] {
			return false
		}
	}
	return true
}

type Client struct {
	Find   bool                `yaml:"Find"` //是否开启服务发现功能
	Server map[string][]string `yaml:"Server"`
}

func (c *Client) Validate(ctx context.Context) bool {
	var valid = true
	if c.Server == nil {
		valid = false
		hlog.Error(ctx, "server is nil")
	}
	return valid
}

// Equal 判断两个Config是否相同
func (c *Client) Equal(other *Client) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}
	if !(c.Find == other.Find && len(c.Server) == len(other.Server)) {
		return false
	}
	for k, v := range c.Server {
		if len(v) != len(other.Server[k]) {
			return false
		}
		for i, val := range v {
			if val != other.Server[k][i] {
				return false
			}
		}
	}
	return true
}
