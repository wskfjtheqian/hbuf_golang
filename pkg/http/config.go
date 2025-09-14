package http

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/utl"
)

var LogHTTP = hlog.INFO + 200

type Config struct {
	Addr *string `yaml:"Addr"`
	Crt  *string `yaml:"Crt"` //crt证书
	Key  *string `yaml:"Key"` //crt密钥
}

func (c *Config) Validate() bool {
	if c == nil {
		hlog.Error("not found http config")
		return false
	}

	valid := true
	if c.Addr == nil || *c.Addr == "" {
		valid = false
		hlog.Error("Addr is empty")
	}
	return valid
}

func (c *Config) Equal(other *Config) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}
	return utl.Equal(c.Addr, other.Addr) &&
		utl.Equal(c.Crt, other.Crt) &&
		utl.Equal(c.Key, other.Key)
}
