package base

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/cache"
	"github.com/wskfjtheqian/hbuf_golang/pkg/db"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	etc "github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/manage"
	"github.com/wskfjtheqian/hbuf_golang/pkg/mq"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"os"
	"sync"

	"reflect"
)

type Context struct {
	context.Context
	app *App
}

func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d.app
	}
	return d.Context.Value(key)
}

func (d *Context) Done() <-chan struct{} {
	return d.Context.Done()
}

var cType = reflect.TypeOf(&Context{})

func GET(ctx context.Context) *App {
	var ret = ctx.Value(cType)
	if nil == ret {
		return nil
	}
	return ret.(*App)
}

type App struct {
	lock         sync.Mutex
	manage       *manage.BaseManage
	db           *db.Database
	cache        *cache.Cache
	etcd         *etc.Etcd
	ext          *rpc.Server
	dataCenterId int64
	workerId     int64
	ctx          context.Context
	config       *Config
	nats         *mq.Nats
}

func NewApp() *App {
	app := &App{
		db:     db.NewDB(),
		cache:  cache.NewCache(),
		etcd:   etc.NewEtcd(),
		nats:   mq.NewNats(),
		manage: manage.NewManage(),
		ext:    rpc.NewServer(),
	}
	app.manage.SetEtcd(app.etcd)
	server := app.ext
	server.PrefixFilter(app.OnFilter)
	server.PrefixFilter(app.etcd.OnFilter)
	server.PrefixFilter(app.nats.OnFilter)
	server.PrefixFilter(app.db.OnFilter)
	server.PrefixFilter(app.manage.OnFilter)
	server.PrefixFilter(app.cache.OnFilter)

	server = app.manage.RpcServer()
	server.PrefixFilter(app.OnFilter)
	server.PrefixFilter(app.etcd.OnFilter)
	server.PrefixFilter(app.nats.OnFilter)
	server.PrefixFilter(app.db.OnFilter)
	server.PrefixFilter(app.manage.OnFilter)
	server.PrefixFilter(app.cache.OnFilter)

	ctx := rpc.NewContext(context.Background())
	rpc.SetContextOnClone(ctx, func(ctx context.Context) (context.Context, error) {
		ctx, _, err := app.ext.GetFilter().OnNext(ctx, nil, nil)
		if err != nil {
			return nil, err
		}
		return ctx, nil
	})
	ctx, _, err := app.ext.GetFilter().OnNext(ctx, nil, nil)
	if err != nil {
		return nil
	}
	app.ctx = ctx
	return app

}
func (a *App) SetConfig(config *Config) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if nil == config {
		a.db.SetConfig(nil)
		a.cache.SetConfig(nil)
		a.etcd.SetConfig(nil)
		a.nats.SetConfig(nil)
		a.manage.SetConfig(nil)
		a.config = nil
		return
	}

	if nil != a.config && a.config.Yaml() == config.Yaml() {
		return
	}
	a.config = config

	a.db.SetConfig(config.DB)
	a.cache.SetConfig(config.Redis)
	a.etcd.SetConfig(config.Etcd)
	a.nats.SetConfig(config.Nats)
	a.manage.SetConfig(config.Server)
	if nil != config {
		a.dataCenterId = config.DataCenterId
		a.workerId = config.WorkerId
	}
}

func (a *App) OnFilter(ctx context.Context, data hbuf.Data, in *rpc.Filter, call rpc.FilterCall) (context.Context, hbuf.Data, error) {
	if nil == ctx.Value(cType) {
		ctx = &Context{
			ctx,
			a,
		}
	}
	return in.OnNext(ctx, data, call)
}

func (a *App) GetEtcd() *etc.Etcd {
	return a.etcd
}

func (a *App) GetManage() *manage.BaseManage {
	return a.manage
}

func (a *App) GetDB() *db.Database {
	return a.db
}

func (a *App) GetCache() *cache.Cache {
	return a.cache
}

func (a *App) GetExt() *rpc.Server {
	return a.ext
}
func (a *App) GetNats() *mq.Nats {
	return a.nats
}
func (a *App) GetDataCenterId() int64 {
	return a.dataCenterId
}

func (a *App) GetWorkerId() int64 {
	return a.workerId
}

func (a *App) CloneContext() context.Context {
	ctx, err := rpc.CloneContext(a.ctx)
	if err != nil {
		erro.PrintStack(err)
		os.Exit(0)
	}
	return ctx
}

func (a *App) Init() {
	ctx := a.CloneContext()
	defer rpc.CloseContext(ctx)
	a.manage.Init(ctx)
}
