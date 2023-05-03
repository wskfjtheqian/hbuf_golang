package mq

import (
	_ "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"
	"log"
	"time"
)

type Config struct {
	Endpoints           []string       `yaml:"endpoints"` // Nats 连接地址
	MaxReconnects       *int           `yaml:"max_reconnects"`
	MaxPingsOutstanding *int           `yaml:"max_pings_outstanding"`
	Name                *string        `yaml:"name"`
	Timeout             *time.Duration `yaml:"timeout"`
	DrainTimeout        *time.Duration `yaml:"drain_timeout"`
	Username            *string        `yaml:"username"`
	Password            *string        `yaml:"password"`
	Token               *string        `yaml:"token"`
	CertFile            *string        `yaml:"cert_file"`
	KeyFile             *string        `yaml:"key_file"`
}

func (c *Config) Yaml() string {
	bytes, err := yaml.Marshal(c)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func (con *Config) CheckConfig() int {
	errCount := 0
	if nil == con.Endpoints || 0 == len(con.Endpoints) {
		errCount++
		log.Println("未找到Nats 连接地址")
	}
	return errCount
}
