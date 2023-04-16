package base

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/cache"
	"github.com/wskfjtheqian/hbuf_golang/pkg/db"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	etc "github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/ip"
	"github.com/wskfjtheqian/hbuf_golang/pkg/manage"
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
	manage       *manage.Manage
	db           *db.Database
	cache        *cache.Cache
	etcd         *etc.Etcd
	ext          *rpc.Server
	dataCenterId int64
	workerId     int64
	ctx          context.Context
	config       *Config
}

func NewApp() *App {
	app := &App{
		db:     db.NewDB(),
		cache:  cache.NewCache(),
		manage: manage.NewManage(),
		ext:    rpc.NewServer(),
		etcd:   etc.NewEtcd(),
	}

	app.ext.PrefixFilter(app.OnFilter)
	app.ext.PrefixFilter(app.etcd.OnFilter)
	app.ext.PrefixFilter(app.db.OnFilter)
	app.ext.PrefixFilter(app.manage.OnFilter)
	app.ext.PrefixFilter(app.cache.OnFilter)
	app.ext.PrefixFilter(app.onHttpFilter)

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
		a.manage.SetConfig(nil)
		a.etcd.SetConfig(nil)
		a.config = nil
		return
	}

	if nil != a.config && a.config.Yaml() == config.Yaml() {
		return
	}
	a.config = config

	a.db.SetConfig(config.DB)
	a.cache.SetConfig(config.Redis)
	a.manage.SetConfig(config.Server)
	a.etcd.SetConfig(config.Etcd)

	if nil != config {
		a.dataCenterId = config.DataCenterId
		a.workerId = config.WorkerId
	}
}

func (a *App) onHttpFilter(ctx context.Context, data hbuf.Data, in *rpc.Filter, call rpc.FilterCall) (context.Context, hbuf.Data, error) {
	jc := rpc.GetHttp(ctx)
	if nil != jc {
		ip, err := ip.GetHttpIP(jc.Request)
		if err != nil {
			return nil, data, err
		}
		rpc.SetHeader(ctx, "IP", ip)
	}
	return in.OnNext(ctx, data, call)
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

func (a *App) GetManage() *manage.Manage {
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
	defer rpc.CloneContext(ctx)
	a.manage.Init(ctx)
}
