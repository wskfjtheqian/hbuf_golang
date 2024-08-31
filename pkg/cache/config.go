package cache

import (
	"github.com/garyburd/redigo/redis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Network         *string `yaml:"network"`            // 网络类型
	Address         *string `yaml:"address"`            // Redis 服务器地址
	Password        *string `yaml:"password"`           // 密码
	MaxIdle         *int    `yaml:"max_idle"`           // 最大空闲链接数 默认8
	MaxActive       *int    `yaml:"max_active"`         // 表示和数据库的最大链接数， 默认0 表示没有限制
	IdleTimeout     *int    `yaml:"idle_timeout"`       // 最大空闲时间  默认0100ms
	Db              int     `yaml:"db"`                 // 数据库ID
	Wait            *bool   `yaml:"wait"`               // 如果 Wait 为真且池处于 MaxActive 限制，则 Get() 等待连接返回池后再返回
	MaxConnLifetime *int    `yaml:"max_conn_life_time"` // 关闭超过此持续时间的连接。如果值为零，则池不会根据年龄关闭连接
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
		hlog.Error("未找到Redis支持的网络类型，请使用 tcp")
	}
	if nil == con.Address || "" == *con.Address {
		errCount++
		hlog.Error("未找到Redis服务器地址")
	}

	conn, err := redis.Dial(*con.Network, *con.Address)
	if err != nil {
		errCount++
		hlog.Error("Redis链接失败，请检查配置是否正确", err)
	}
	defer func(c redis.Conn) {
		_ = c.Close()
	}(conn)

	if nil != con.Password && 0 != len(*con.Password) {
		_, err := conn.Do("AUTH", *con.Password)
		if err != nil {
			errCount++
			hlog.Error("Redis 认证失败，请检查密码是否正确", err)
		}
	}
	hlog.Info("Redis 检查：Ok")
	return errCount
}
