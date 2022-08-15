package base

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/cache"
	"github.com/wskfjtheqian/hbuf_golang/pkg/db"
	"github.com/wskfjtheqian/hbuf_golang/pkg/manage"
	"log"
)

type Config struct {
	Redis        *cache.Config  `yaml:"redis"`
	DB           *db.Config     `yaml:"db"`
	Service      *manage.Config `yaml:"service"`
	WorkerId     int64          `yaml:"worker_id"`
	DataCenterId int64          `yaml:"data_center_id"`
}

func (con *Config) CheckConfig() int {
	errCount := 0

	if nil == con.Redis {
		errCount++
		log.Println("未找到Redis的配置文件")
	} else {
		errCount += con.Redis.CheckConfig()
	}
	if nil == con.DB {
		errCount++
		log.Println("未找到数据库的配置文件")
	} else {
		errCount += con.DB.CheckConfig()
	}

	if 0 == con.DataCenterId {
		errCount++
		log.Println("机房ID设置错误，请设置 data_center_id 大于 0")
	}

	if 0 == con.WorkerId {
		errCount++
		log.Println("机器ID设置错误，请设置 worker_id 大于 0")
	}
	return errCount
}
