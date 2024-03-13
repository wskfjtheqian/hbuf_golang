package cache

import (
	"github.com/garyburd/redigo/redis"
	"github.com/golang/glog"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Network     *string `yaml:"network"`      // 网络类型
	Address     *string `yaml:"address"`      // Redis 服务器地址
	Password    *string `yaml:"password"`     // 密码
	MaxIdle     *int    `yaml:"max_idle"`     // 最大空闲链接数 默认8
	MaxActive   *int    `yaml:"max_active"`   // 表示和数据库的最大链接数， 默认0 表示没有限制
	IdleTimeout *int    `yaml:"idle_timeout"` // 最大空闲时间  默认0100ms
	Db          int     `yaml:"db"`           // 数据库ID
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
	if nil == con.Network || !("tcp" == *con.Network) {
		errCount++
		glog.Errorln("未找到Redis支持的网络类型，请使用 tcp")
	}
	if nil == con.Address || "" == *con.Address {
		errCount++
		glog.Errorln("未找到Redis服务器地址")
	}

	conn, err := redis.Dial(*con.Network, *con.Address)
	if err != nil {
		errCount++
		glog.Errorln("Redis链接失败，请检查配置是否正确", err)
	}
	defer func(c redis.Conn) {
		_ = c.Close()
	}(conn)

	if nil != con.Password && 0 != len(*con.Password) {
		_, err := conn.Do("AUTH", *con.Password)
		if err != nil {
			errCount++
			glog.Errorln("Redis 认证失败，请检查密码是否正确", err)
		}
	}
	glog.Infoln("Redis 检查：Ok")
	return errCount
}
