package happ

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

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

func WithCloseDuration(duration time.Duration) Option {
	return func(s *App) {
		s.closeDuration = duration
	}
}

// NewApp 新建一个App
func NewApp(options ...Option) *App {
	ret := &App{
		middleware: func(next hrpc.Handler) hrpc.Handler {
			return next
		},
		ctx:           hlog.NewContext(),
		closeDuration: 30 * time.Second,
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

	service       *hservice.Service
	middleware    func(next hrpc.Handler) hrpc.Handler
	ctx           context.Context
	closeDuration time.Duration
	wait          sync.WaitGroup
	onShutdown    []func(ctx context.Context) error
}

func (a *App) SetOnShutdown(onShutdown ...func(ctx context.Context) error) {
	a.onShutdown = onShutdown
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
	a.wait.Add(1)
	go func() {
		defer func() {
			err := recover()
			if err != nil {
				hlog.Error(ctx, "%s \n", err, string(debug.Stack()))
			}
			a.wait.Done()
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

func (a *App) Exec(ctx context.Context, fn func(ctx context.Context) error) error {
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

func (a *App) Run(fn func(ctx context.Context) error) {
	// 1. 创建可取消的 context
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		select {
		case <-ctx.Done():
			break
		case <-quit:
			hlog.Info(a.ctx, "shutdown server ...")
			cancel()
			break
		}
	}()

	err := fn(ctx)
	if err != nil {
		herror.PrintStack(a.ctx, err)
	}

	<-ctx.Done()
	hlog.Info(ctx, "waiting app to shutdown")

	shutdownCtx, shutdownCancel := context.WithTimeout(hlog.WithContext(context.Background(), hlog.FromContext(a.ctx)), a.closeDuration)
	defer shutdownCancel()

	// 先关闭第一批（如流量）
	err = a.service.Deregister(shutdownCtx)
	if err != nil {
		hlog.Error(a.ctx, "Deregister failed: %v", err)
	}

	if len(a.onShutdown) > 0 && a.onShutdown[0] != nil {
		err = a.onShutdown[0](shutdownCtx)
		if err != nil {
			hlog.Error(ctx, "onShutdown 1 error: %v", err)
		}
	}

	err = a.service.Shutdown(shutdownCtx)
	if err != nil {
		hlog.Error(ctx, "service close error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		a.wait.Wait()
		close(done)
	}()

	select {
	case <-done:
		hlog.Info(a.ctx, "all goroutines completed")
	case <-shutdownCtx.Done():
		hlog.Warn(a.ctx, "shutdown timeout after %v, forcing exit", a.closeDuration)
		a.printRunningGoroutines()
	}

	// 再关闭第二批（如数据库）
	for i := 1; i < len(a.onShutdown); i++ {
		if a.onShutdown[i] != nil {
			err = a.onShutdown[i](a.ctx)
			if err != nil {
				hlog.Error(ctx, "onShutdown %d error: %v", i, err)
			}
		}
	}

	err = a.sqlDb.Shutdown(a.ctx)
	if err != nil {
		hlog.Error(a.ctx, "sqlDb close error: %v", err)
	}

	err = a.redis.Shutdown(a.ctx)
	if err != nil {
		hlog.Error(a.ctx, "redis close error: %v", err)
	}

	err = a.nats.Shutdown(a.ctx)
	if err != nil {
		hlog.Error(a.ctx, "nats close error: %v", err)
	}

	err = a.etcd.Shutdown(ctx)
	if err != nil {
		hlog.Error(a.ctx, "etcd close error: %v", err)
	}

	hlog.Info(a.ctx, "shutdown completed")
	return
}

// printRunningGoroutines 打印还在运行的 goroutine（调试用）
func (a *App) printRunningGoroutines() {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	hlog.Warn(a.ctx, "running goroutines:\n%s", buf[:n])
}
