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

func (h *Config) Validate() bool {
	valid := true
	if h.Addr == nil || *h.Addr == "" {
		valid = false
		hlog.Error("Addr is empty")
	}
	return valid
}

func (h *Config) Equal(other *Config) bool {
	if h == nil && other == nil {
		return true
	}
	if h == nil || other == nil {
		return false
	}
	return utl.Equal(h.Addr, other.Addr) &&
		utl.Equal(h.Crt, other.Crt) &&
		utl.Equal(h.Key, other.Key)
}
