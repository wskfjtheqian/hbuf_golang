package app

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/nats"
	"github.com/wskfjtheqian/hbuf_golang/pkg/redis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/sql"
)

// NewApp 新建一个App
func NewApp() *App {
	return &App{
		nats:  nats.NewNats(),
		etcd:  etcd.NewEtcd(),
		redis: redis.NewRedis(),
		sqlDb: sql.NewDB(),
	}
}

// App 应用
type App struct {
	nats  *nats.Nats
	etcd  *etcd.Etcd
	redis *redis.Redis
	sqlDb *sql.Sql
}

// SetConfig 设置配置
func (a *App) SetConfig(conf *Config) error {
	err := a.nats.SetConfig(conf.Nats)
	if err != nil {
		return err
	}

	err = a.etcd.SetConfig(conf.Etcd)
	if err != nil {
		return err
	}

	err = a.redis.SetConfig(conf.Redis)
	if err != nil {
		return err
	}

	err = a.sqlDb.SetConfig(conf.Sql)
	if err != nil {
		return err
	}
	return nil
}
