package base

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/cache"
	"github.com/wskfjtheqian/hbuf_golang/pkg/db"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	etc "github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/ip"
	"github.com/wskfjtheqian/hbuf_golang/pkg/manage"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"log"
	"os"

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
	manage       *manage.Manage
	db           *db.Database
	cache        *cache.Cache
	etcd         *etc.Etcd
	ext          *rpc.Server
	dataCenterId int64
	workerId     int64
	ctx          context.Context
}

func NewApp(con *Config) *App {
	app := &App{
		db:           db.NewDB(con.DB),
		cache:        cache.NewCache(con.Redis),
		manage:       manage.NewManage(con.Service),
		ext:          rpc.NewServer(),
		etcd:         etc.NewEtcd(con.Etcd),
		dataCenterId: con.DataCenterId,
		workerId:     con.WorkerId,
	}
	app.ext.AddFilter(app.OnFilter)
	app.ext.AddFilter(app.onHttpFilter)
	ctx, err := app.OnFilter(rpc.NewContext(context.Background()))
	if err != nil {
		log.Fatalln("Init base app error:", err)
	}
	rpc.SetContextOnClone(ctx, func(ctx context.Context) (context.Context, error) {
		c, err := app.OnFilter(ctx)
		if err != nil {
			return nil, err
		}
		return c, nil
	})

	app.ctx = ctx
	return app
}

func (a *App) onHttpFilter(ctx context.Context) (context.Context, error) {
	jc := rpc.GetHttp(ctx)
	if nil == jc {
		return ctx, nil
	}
	ip, err := ip.GetHttpIP(jc.Request)
	if err != nil {
		return nil, err
	}
	rpc.SetHeader(ctx, "IP", ip)
	return ctx, nil
}

func (a *App) OnFilter(ctx context.Context) (context.Context, error) {
	if nil == ctx.Value(cType) {
		ctx = &Context{
			ctx,
			a,
		}
	}

	ctx, err := a.etcd.OnFilter(ctx)
	if err != nil {
		return nil, err
	}
	ctx, err = a.db.OnFilter(ctx)
	if err != nil {
		return nil, err
	}
	ctx, err = a.manage.OnFilter(ctx)
	if err != nil {
		return nil, err
	}
	ctx, err = a.cache.OnFilter(ctx)
	if err != nil {
		return nil, err
	}
	return ctx, nil
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
	a.manage.Init()
}
