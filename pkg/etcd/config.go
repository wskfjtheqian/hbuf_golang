package etc

import (
	_ "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"
	"log"
	"time"
)

type Config struct {
	Endpoints   []string       `yaml:"endpoints"`    // Etcd 连接地址
	DialTimeout *time.Duration `yaml:"dial_timeout"` //
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
		log.Println("未找到Etcd 连接地址")
	}
	return errCount
}
