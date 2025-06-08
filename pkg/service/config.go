package service

import (
	"crypto/tls"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

type Config struct {
	Server *Server `yaml:"Server"` //服务端配置
	Client *Client `yaml:"Client"` //客户端配置
}

// Equal 判断两个Config是否相同
func (c *Config) Equal(other *Config) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}

	if c.Server.Equal(other.Server) && c.Client.Equal(other.Client) {
		return true
	}
	return false
}

// Validate 检查配置是否有效
func (c *Config) Validate() bool {
	var valid = true
	if c.Server == nil {
		valid = false
		hlog.Error("server config is nil")
	}
	if c.Client == nil {
		valid = false
		hlog.Error("client config is nil")
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
func (h *Http) Validate() bool {
	var valid = true
	if h.Hostname == nil || *h.Hostname == "" {
		valid = false
		hlog.Error("hostname is nil")
	}
	if h.Address == nil || *h.Address == "" {
		valid = false
		hlog.Error("address is nil")
	}

	if h.Crt != nil && *h.Crt != "" && h.Key != nil && *h.Key != "" {
		_, err := tls.LoadX509KeyPair(*h.Crt, *h.Key)
		if err != nil {
			hlog.Error("load x509 key pair error: %s", err.Error())
			valid = false
		}
	}

	if h.Path == nil {
		valid = false
		hlog.Error("path is nil")
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
func (s *Server) Validate() bool {
	var valid = true
	if s.Http == nil {
		valid = false
		hlog.Error("http config is nil")
	} else {
		valid = s.Http.Validate()
	}
	if s.List == nil {
		valid = false
		hlog.Error("list is nil")
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

func (c *Client) Validate() bool {
	var valid = true
	if c.Server == nil {
		valid = false
		hlog.Error("server is nil")
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
