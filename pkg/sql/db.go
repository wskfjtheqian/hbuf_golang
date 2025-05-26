package sql

import (
	"context"
	"database/sql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"reflect"
	"sync/atomic"
)

// WithContext 给上下文添加 Builder 连接
func WithContext(ctx context.Context, n *DB) context.Context {
	return &Context{
		Context: ctx,
		db:      n,
	}
}

// Context 定义了 Builder 的上下文
type Context struct {
	context.Context
	db *DB
}

var contextType = reflect.TypeOf(&Context{})

// Value 返回Context的value
func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

// FromContext 从上下文中获取 Builder 连接
func FromContext(ctx context.Context) (n *DB, ok bool) {
	val := ctx.Value(contextType)
	if val == nil {
		return nil, false
	}
	return val.(*Context).db, true
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func NewDB() *DB {
	return &DB{}
}

type DB struct {
	config *Config
	db     atomic.Pointer[sql.DB]
}

func (d *DB) SetConfig(cfg *Config) error {
	if d.config.Equal(cfg) {
		return nil
	}
	if cfg == nil {
		if d.db.Load() != nil {
			conn := d.db.Swap(nil)
			_ = conn.Close()
		}
		d.config = nil
		return nil
	}

	d.config = cfg

	db, err := sql.Open(*cfg.Type, *cfg.Username+":"+*cfg.Password+"@"+*cfg.URL+"&parseTime=true&clientFoundRows=true")
	if err != nil {
		return erro.Wrap(err)
	}

	if cfg.MaxOpenConns != nil {
		db.SetMaxOpenConns(*cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != nil {
		db.SetMaxIdleConns(*cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != nil {
		db.SetConnMaxLifetime(*cfg.ConnMaxLifetime)
	}
	if cfg.MaxIdleConns != nil {
		db.SetMaxIdleConns(*cfg.MaxIdleConns)
	}
	if cfg.ConnMaxIdleTime != nil {
		db.SetConnMaxIdleTime(*cfg.ConnMaxIdleTime)
	}

	if err := db.Ping(); err != nil {
		return erro.Wrap(err)
	}
	hlog.Info("database connected")
	d.db.Store(db)
	return nil
}

func (d *DB) GetDB() (*sql.DB, error) {
	db := d.db.Load()
	if db == nil {
		return nil, erro.NewError("database not initialized")
	}
	return db, nil
}

func (d *DB) NewMiddleware() rpc.HandlerMiddleware {
	return func(next rpc.Handler) rpc.Handler {
		return func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
			return next(WithContext(ctx, d), req)
		}
	}
}
