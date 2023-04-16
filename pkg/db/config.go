package db

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/config"
	"gopkg.in/yaml.v3"
	"log"
)

type ConfigValue struct {
	Type        *string `yaml:"type"`         // 数据库类型
	URL         *string `yaml:"url"`          // 数据库链接
	Username    *string `yaml:"username"`     // 用户名
	Password    *string `yaml:"password"`     // 密码
	MaxIdle     *int    `yaml:"max_idle"`     // 最大空闲链接数 默认8
	MaxActive   *int    `yaml:"max_active"`   // 表示和数据库的最大链接数， 默认0 表示没有限制
	IdleTimeout *int    `yaml:"idle_timeout"` // 最大空闲时间  默认0100ms
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
	if nil == con.Type || !("mysql" == *con.Type) {
		errCount++
		log.Println("未找到支持的数据库类型，请使用 mysql")
	}
	if nil == con.Username || "" == *con.Username {
		errCount++
		log.Println("未找到数据库用户名")
	}
	if nil == con.Password || "" == *con.Password {
		errCount++
		log.Println("未找到数据库密码")
	}
	if nil == con.URL || "" == *con.URL {
		errCount++
		log.Println("未找到数据库链接")
	}

	db, err := sql.Open(*con.Type, *con.Username+":"+*con.Password+"@"+*con.URL)
	if err != nil {
		errCount++
		log.Fatalln("数据库链接失败，请检查配置是否正确", err)
	}
	defer func(db *sql.DB) {
		_ = db.Close()
	}(db)
	log.Println("数据库链接 检查：Ok")
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
