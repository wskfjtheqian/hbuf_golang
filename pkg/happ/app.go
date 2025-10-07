package happ

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hetcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hmq"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hredis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hservice"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hsql"
)

// Option 应用选项
type Option func(*App)

func WithMiddleware(middlewares ...hrpc.HandlerMiddleware) Option {
	return func(s *App) {
		hservice.WithMiddleware(append(s.Middlewares(), middlewares...)...)(s.service)
	}
}

func WithDbCache(cache hsql.DbCache) Option {
	return func(s *App) {
		hsql.WithCache(cache)(s.sqlDb)
	}
}

// NewApp 新建一个App
func NewApp(options ...Option) *App {
	ret := &App{}
	ret.nats = hmq.NewNats()
	ret.etcd = hetcd.NewEtcd()
	ret.redis = hredis.NewRedis()
	ret.sqlDb = hsql.NewDB(hsql.WithCache(hredis.NewDBCache()))
	ret.service = hservice.NewService(ret.etcd, hservice.WithMiddleware())

	for _, option := range options {
		option(ret)
	}
	return ret
}

// App 应用
type App struct {
	nats  *hmq.Nats
	etcd  *hetcd.Etcd
	redis *hredis.Redis
	sqlDb *hsql.DB

	service *hservice.Service
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

func (a *App) Service() *hservice.Service {
	return a.service
}

func (a *App) Middlewares() []hrpc.HandlerMiddleware {
	return []hrpc.HandlerMiddleware{
		a.nats.NewMiddleware(),
		a.etcd.NewMiddleware(),
		a.redis.NewMiddleware(),
		a.sqlDb.NewMiddleware(),
		a.service.NewMiddleware(),
	}
}
