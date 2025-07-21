package db

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Type            *string `yaml:"type"`              // 数据库类型
	DbName          *string `yaml:"db_name"`           // 数据库名称
	Params          *string `yaml:"params"`            // 数据库连接参数
	Host            *string `yaml:"host"`              // 数据库服务器地址
	Network         *string `yaml:"network"`           // 网络类型
	Username        *string `yaml:"username"`          // 用户名
	Password        *string `yaml:"password"`          // 密码
	MaxIdle         *int    `yaml:"max_idle"`          // 最大空闲链接数 默认8
	MaxActive       *int    `yaml:"max_active"`        // 表示和数据库的最大链接数， 默认0 表示没有限制
	IdleTimeout     *int    `yaml:"idle_timeout"`      // 最大空闲时间  默认 100ms
	ConnMaxLifetime *int    `yaml:"conn_max_lifetime"` // 设置数据库闲链接超时时间 默认 20000ms
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
	if nil == con.Type || !("mysql" == *con.Type) {
		errCount++
		hlog.Error("未找到支持的数据库类型，请使用 mysql")
	}
	if nil == con.Username || "" == *con.Username {
		errCount++
		hlog.Error("未找到数据库用户名")
	}
	if nil == con.Password || "" == *con.Password {
		errCount++
		hlog.Error("未找到数据库密码")
	}
	if nil == con.DbName || "" == *con.DbName {
		errCount++
		hlog.Error("未找到数据库链接")
	}
	if nil == con.Host || "" == *con.Host {
		errCount++
		hlog.Error("未找到数据库服务器地址")
	}
	if nil == con.Network || "" == *con.Network {
		errCount++
		hlog.Error("未找到网络类型")
	}
	if nil == con.Params || "" == *con.Params {
		errCount++
		hlog.Error("未找到数据库连接参数")
	}
	if !(nil == con.Username || nil == con.Password || nil == con.DbName || nil == con.Host || nil == con.Network || nil == con.Params) {
		db, err := sql.Open(*con.Type, con.Source())
		if err != nil {
			errCount++
			hlog.Error("数据库链接失败，请检查配置是否正确", err)
		}
		err = db.Ping()
		if err != nil {
			errCount++
			hlog.Error("数据库链接失败，请检查配置是否正确", err)
		}
		defer func(db *sql.DB) {
			_ = db.Close()
		}(db)
	}
	if errCount == 0 {
		hlog.Info("Mysql 检查：Ok")
	}
	return errCount
}

func (con *Config) Source() string {
	return *con.Username + ":" + *con.Password + "@" + *con.Network + "(" + *con.Host + ")/" + *con.DbName + "?" + *con.Params
}

func (con *Config) GetType() string {
	if con.Type == nil {
		return ""
	}
	return *con.Type
}

func (con *Config) GetDbName() string {
	if con.DbName == nil {
		return ""
	}
	return *con.DbName
}

func (con *Config) GetParams() string {
	if con.Params == nil {
		return ""
	}
	return *con.Params
}

func (con *Config) GetHost() string {
	if con.Host == nil {
		return ""
	}
	return *con.Host
}

func (con *Config) GetNetwork() string {
	if con.Network == nil {
		return ""
	}
	return *con.Network
}

func (con *Config) GetUsername() string {
	if con.Username == nil {
		return ""
	}
	return *con.Username
}
