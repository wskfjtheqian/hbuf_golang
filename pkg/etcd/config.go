package etc

import (
	_ "github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/config"
	"gopkg.in/yaml.v3"
	"log"
	"time"
)

type ConfigValue struct {
	Endpoints   []string       `yaml:"endpoints"`    // Etcd 连接地址
	DialTimeout *time.Duration `yaml:"dial_timeout"` //
}

func (c *ConfigValue) Yaml() string {
	bytes, err := yaml.Marshal(c)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func (con *ConfigValue) CheckConfig() int {
	errCount := 0
	if nil == con.Endpoints || 0 == len(con.Endpoints) {
		errCount++
		log.Println("未找到Etcd 连接地址")
	}
	return errCount
}

type Config struct {
	config.Config
}

func (c *Config) OnChange(call func(v *ConfigValue)) {
	c.Config.OnChange(func(value config.Value) {
		if nil != call {
			call(value.(*ConfigValue))
		}
	})
}
