package base

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/cache"
	"github.com/wskfjtheqian/hbuf_golang/pkg/db"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/http"
	"github.com/wskfjtheqian/hbuf_golang/pkg/ip"
	"github.com/wskfjtheqian/hbuf_golang/pkg/manage"

	"reflect"
)

type appContext struct {
	context.Context
	app *App
}

func (d *appContext) Value(key interface{}) interface{} {
	if reflect.TypeOf(d) == key {
		return d.app
	}
	return d.Context.Value(key)
}

func (d *appContext) Done() <-chan struct{} {
	return d.Context.Done()
}

var appType = reflect.TypeOf(&appContext{})

func GET(ctx context.Context) *App {
	var ret = ctx.Value(appType)
	if nil == ret {
		return nil
	}
	return ret.(*App)
}

type App struct {
	manage       *manage.Manage
	db           *db.Database
	cache        *cache.Cache
	ext          *hbuf.Server
	dataCenterId int64
	workerId     int64
}

func NewApp(con *Config) *App {
	app := &App{
		db:           db.NewDB(con.DB),
		cache:        cache.NewCache(con.Redis),
		manage:       manage.NewManage(con.Service),
		ext:          hbuf.NewServer(),
		dataCenterId: con.DataCenterId,
		workerId:     con.WorkerId,
	}
	app.ext.AddFilter(app.OnFilter)
	app.ext.AddFilter(app.onHttpFilter)

	return app
}

func (a *App) onHttpFilter(ctx context.Context) (context.Context, error) {
	jc := http.Get(ctx)
	if nil == jc {
		return ctx, nil
	}
	ip, err := ip.GetHttpIP(jc.Request)
	if err != nil {
		return nil, err
	}
	hbuf.SetHeader(ctx, "IP", ip)
	return ctx, nil
}

func (a *App) OnFilter(ctx context.Context) (context.Context, error) {
	if nil == ctx.Value(appType) {
		ctx = &appContext{
			ctx,
			a,
		}
	}
	ctx, err := a.db.OnFilter(ctx)
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

func (a *App) GetExt() *hbuf.Server {
	return a.ext
}

func (a *App) GetDataCenterId() int64 {
	return a.dataCenterId
}

func (a *App) GetWorkerId() int64 {
	return a.workerId
}
