package happ

import (
	"context"
	"runtime/debug"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hetcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hmq"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hredis"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hservice"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hsql"
)

// Option 应用选项
type Option func(*App)

func WithServiceOption(options ...hservice.Option) Option {
	return func(s *App) {
		for _, option := range options {
			option(s.service)
		}
	}
}

func WithMqOption(options ...hmq.Option) Option {
	return func(s *App) {
		for _, option := range options {
			option(s.nats)
		}
	}
}

func WithMiddleware(middlewares ...hrpc.Middleware) Option {
	return func(s *App) {
		s.middleware = func(next hrpc.Handler) hrpc.Handler {
			for i := len(middlewares) - 1; i >= 0; i-- {
				next = middlewares[i](next)
			}
			return next
		}
	}
}

// NewApp 新建一个App
func NewApp(options ...Option) *App {
	ret := &App{
		middleware: func(next hrpc.Handler) hrpc.Handler {
			return next
		},
	}
	ret.nats = hmq.NewNats()
	ret.etcd = hetcd.NewEtcd()
	ret.redis = hredis.NewRedis()
	ret.sqlDb = hsql.NewDB(hsql.WithCache(hredis.NewCache("db")))
	ret.service = hservice.NewService(ret.etcd, hservice.WithServerOption(hrpc.WithServerMiddleware(ret.Middlewares()...)))

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

	service    *hservice.Service
	middleware func(next hrpc.Handler) hrpc.Handler
}

// SetConfig 设置配置
func (a *App) SetConfig(ctx context.Context, conf *Config) error {
	err := a.nats.SetConfig(ctx, conf.Nats)
	if err != nil {
		return err
	}

	err = a.etcd.SetConfig(ctx, conf.Etcd)
	if err != nil {
		return err
	}

	err = a.redis.SetConfig(ctx, conf.Redis)
	if err != nil {
		return err
	}

	err = a.sqlDb.SetConfig(ctx, conf.Sql)
	if err != nil {
		return err
	}

	err = a.service.SetConfig(ctx, conf.Service)
	if err != nil {
		return err
	}
	return nil
}

func (a *App) Init(ctx context.Context) {

}

func (a *App) Service() *hservice.Service {
	return a.service
}

func (a *App) Middlewares() []hrpc.Middleware {
	return []hrpc.Middleware{
		a.nats.NewMiddleware(),
		a.etcd.NewMiddleware(),
		a.redis.NewMiddleware(),
		a.sqlDb.NewMiddleware(),
		a.service.NewMiddleware(),
	}
}

func (a *App) Go(ctx context.Context, fn func(ctx context.Context) error) {
	go func() {
		defer func() {
			err := recover()
			if err != nil {
				hlog.Error(ctx, "%s \n", err, string(debug.Stack()))
			}
		}()
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()

		ctx = hlog.WithContext(ctx, hlog.FromContext(ctx))
		_, err := a.middleware(func(ctx context.Context, req any) (any, error) {
			return nil, fn(ctx)
		})(ctx, nil)
		if err != nil {
			herror.PrintStack(ctx, err)
		}
	}()
}

func (a *App) Run(ctx context.Context, fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	_, err := a.middleware(func(ctx context.Context, req any) (any, error) {
		return nil, fn(ctx)
	})(ctx, nil)
	if err != nil {
		return err
	}
	return nil
}
