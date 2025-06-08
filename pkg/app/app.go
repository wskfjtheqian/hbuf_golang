package app

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/nats"
	"github.com/wskfjtheqian/hbuf_golang/pkg/redis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/service"
	"github.com/wskfjtheqian/hbuf_golang/pkg/sql"
)

// NewApp 新建一个App
func NewApp() *App {
	ret := &App{
		nats:  nats.NewNats(),
		etcd:  etcd.NewEtcd(),
		redis: redis.NewRedis(),
		sqlDb: sql.NewDB(),
	}

	ret.service = service.NewService(ret.etcd, []rpc.HandlerMiddleware{
		ret.nats.NewMiddleware(),
		ret.etcd.NewMiddleware(),
		ret.redis.NewMiddleware(),
		ret.sqlDb.NewMiddleware(),
	})

	return ret
}

// App 应用
type App struct {
	nats  *nats.Nats
	etcd  *etcd.Etcd
	redis *redis.Redis
	sqlDb *sql.DB

	service *service.Service
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

	err = a.service.SetConfig(conf.Service)
	if err != nil {
		return err
	}
	return nil
}

func (a *App) Init() {

}

func (a *App) Service() *service.Service {
	return a.service
}

func (a *App) Middlewares() []rpc.HandlerMiddleware {
	return []rpc.HandlerMiddleware{
		a.nats.NewMiddleware(),
		a.etcd.NewMiddleware(),
		a.redis.NewMiddleware(),
		a.sqlDb.NewMiddleware(),
		a.service.NewMiddleware(),
	}
}
