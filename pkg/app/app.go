package app

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/nats"
	"github.com/wskfjtheqian/hbuf_golang/pkg/redis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/service"
	"github.com/wskfjtheqian/hbuf_golang/pkg/sql"
)

// Option 应用选项
type Option func(*App)

func WithMiddleware(middlewares ...rpc.HandlerMiddleware) Option {
	return func(s *App) {
		service.WithMiddleware(append(s.Middlewares(), middlewares...)...)(s.service)
	}
}

func WithDbCache(cache sql.DbCache) Option {
	return func(s *App) {
		sql.WithCache(cache)(s.sqlDb)
	}
}

// NewApp 新建一个App
func NewApp(options ...Option) *App {
	ret := &App{}
	ret.nats = nats.NewNats()
	ret.etcd = etcd.NewEtcd()
	ret.redis = redis.NewRedis()
	ret.sqlDb = sql.NewDB(sql.WithCache(redis.NewDBCache()))
	ret.service = service.NewService(ret.etcd, service.WithMiddleware())

	for _, option := range options {
		option(ret)
	}
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
