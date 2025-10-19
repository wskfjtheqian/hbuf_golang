package hsql

import (
	"context"
	"database/sql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
	"net/url"
	"reflect"
	"sync/atomic"
	"time"
)

type Sql interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
}

// WithContext 给上下文添加 Builder 连接
func WithContext(ctx context.Context, n *DB, tableNameFunc func(ctx context.Context, name string) string) context.Context {
	ret := &Context{
		Context: ctx,
		db:      n,
		tableNameFunc: func(ctx context.Context, name string) string {
			return name
		},
	}
	if tableNameFunc != nil {
		ret.tableNameFunc = tableNameFunc
	}
	return ret
}

// Context 定义了 Builder 的上下文
type Context struct {
	context.Context
	db            *DB
	tx            *sql.Tx
	tableNameFunc func(ctx context.Context, name string) string
}

var contextType = reflect.TypeOf(&Context{})

// Value 返回Context的value
func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

func (d *Context) GetDB() (Sql, error) {
	if d.tx != nil {
		return d.tx, nil
	}
	db := d.db.db.Load()
	if db == nil {
		return nil, herror.NewError("database not initialized")
	}
	return db, nil
}

func (d *Context) GetConfig() *Config {
	return d.db.config
}

func (d *Context) GetCache() DbCache {
	return d.db.cache
}

// FromContext 从上下文中获取 Builder 连接
func FromContext(ctx context.Context) (n *Context, ok bool) {
	val := ctx.Value(contextType)
	if val == nil {
		return nil, false
	}
	return val.(*Context), true
}

// TableName 获取表名的函数
func TableName(ctx context.Context, name string) string {
	val := ctx.Value(contextType)
	if val == nil {
		return name
	}
	return val.(*Context).tableNameFunc(ctx, name)
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type Option func(*DB)

func WithCache(cache DbCache) Option {
	return func(db *DB) {
		db.cache = cache
	}
}

func NewDB(option ...Option) *DB {
	ret := &DB{}
	for _, opt := range option {
		opt(ret)
	}
	return ret
}

type DB struct {
	config *Config
	db     atomic.Pointer[sql.DB]
	cache  DbCache
}

func (d *DB) SetConfig(cfg *Config) error {
	if d.config.Equal(cfg) {
		return nil
	}
	old := d.db.Load()
	defer func() {
		if old != nil {
			<-time.After(time.Second * 30)
			_ = old.Close()
			hlog.Info("old database client closed")
		}
	}()

	if cfg == nil {
		if old != nil {
			conn := d.db.Swap(nil)
			_ = conn.Close()
		}
		d.config = nil
		return nil
	}

	d.config = cfg

	uri := *cfg.Username + ":" + *cfg.Password + "@" + *cfg.Network + "(" + *cfg.Host + ")/" + *cfg.DbName

	query, err := url.ParseQuery(*cfg.Params)
	if err != nil {
		return herror.Wrap(err)
	}
	query.Set("parseTime", "true")
	query.Set("clientFoundRows", "true")
	uri += "?" + query.Encode()

	db, err := sql.Open(*cfg.Type, uri)
	if err != nil {
		return herror.Wrap(err)
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
		return herror.Wrap(err)
	}
	hlog.Info("database client connected")
	d.db.Store(db)
	return nil
}

func (d *DB) NewMiddleware() hrpc.HandlerMiddleware {
	return func(next hrpc.Handler) hrpc.Handler {
		return func(ctx context.Context, req any) (any, error) {
			return next(WithContext(ctx, d, nil), req)
		}
	}
}

// Transaction 事务函数
func Transaction(ctx context.Context, fn func(ctx context.Context) error) error {
	val := ctx.Value(contextType)
	if val == nil {
		return herror.NewError("not found Db in context")
	}

	if val.(*Context).tx != nil {
		return fn(ctx)
	}

	db, err := val.(*Context).GetDB()
	if err != nil {
		return err
	}
	tx, err := (db.(*sql.DB)).Begin()
	if err != nil {
		return herror.Wrap(err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		} else if err != nil {
			_ = tx.Rollback()
		}
	}()

	ctx = &Context{
		Context:       ctx,
		db:            val.(*Context).db,
		tableNameFunc: val.(*Context).tableNameFunc,
		tx:            tx,
	}

	err = fn(ctx)
	if err != nil {
		return err
	}
	return nil
}
