package etc

import (
	_ "github.com/go-sql-driver/mysql"
	"log"
	"time"
)

type Config struct {
	Endpoints   []string       `yaml:"endpoints"`    // Etcd 连接地址
	DialTimeout *time.Duration `yaml:"dial_timeout"` //
}

func (con *Config) CheckConfig() int {
	errCount := 0
	if nil == con.Endpoints || 0 == len(con.Endpoints) {
		errCount++
		log.Println("未找到Etcd 连接地址")
	}
	return errCount
}
