package db

import (
	"context"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"reflect"
	"sync"
	"time"
)

type Execute interface {
	Query(query string, args ...any) (*sql.Rows, error)

	Exec(query string, args ...any) (sql.Result, error)

	Table(name string) string

	Db() *DB
}

type DB struct {
	*sql.DB

	config Config
}

func (D *DB) Config() Config {
	return D.config
}

type Context struct {
	context.Context

	db *DB

	tx *Tx

	tableName func(ctx context.Context, name string) string
}

func NewContext(context context.Context, db *DB, tableName func(ctx context.Context, name string) string) *Context {
	return &Context{
		Context:   context,
		db:        db,
		tableName: tableName,
	}
}

func (c *Context) Query(query string, args ...any) (*sql.Rows, error) {
	if nil != c.tx {
		rows, err := c.tx.t.Query(query, args...)
		if err != nil {
			return nil, erro.Wrap(err)
		}
		return rows, nil
	}
	rows, err := c.db.DB.Query(query, args...)
	if err != nil {
		return nil, erro.Wrap(err)
	}
	return rows, nil
}

func (c *Context) Exec(query string, args ...any) (sql.Result, error) {
	if nil != c.tx {
		exec, err := c.tx.t.Exec(query, args...)
		if err != nil {
			return nil, erro.Wrap(err)
		}
		return exec, nil
	}
	exec, err := c.db.DB.Exec(query, args...)
	if err != nil {
		return nil, erro.Wrap(err)
	}
	return exec, nil
}

func (c *Context) Table(name string) string {
	return c.tableName(c, name)
}

func (c *Context) Db() *DB {
	return c.db
}

var cType = reflect.TypeOf(&Context{})

func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

func (d *Context) Done() <-chan struct{} {
	return d.Context.Done()
}

func GET(ctx context.Context) Execute {
	var ret = ctx.Value(cType)
	if nil == ret {
		return nil
	}
	return ret.(*Context)
}

type Database struct {
	db     *DB
	config *Config
	lock   sync.Mutex
}

func (d *Database) OnFilter(ctx context.Context, data hbuf.Data, in *rpc.Filter, call rpc.FilterCall) (context.Context, hbuf.Data, error) {
	if nil == ctx.Value(cType) {
		ctx = &Context{
			Context: ctx,
			db:      d.db,
			tx:      nil,
			tableName: func(ctx context.Context, name string) string {
				return name
			},
		}
	}
	return in.OnNext(ctx, data, call)
}

func (d *Database) SetConfig(config *Config) {
	d.lock.Lock()
	defer d.lock.Unlock()
	if nil == config {
		if nil != d.db {
			d.db.DB.Close()
		}
		d.db = nil
		d.config = nil
		return
	}
	if nil != d.config && d.config.Yaml() == config.Yaml() {
		return
	}
	d.config = config

	db, err := sql.Open(*config.Type, config.Source())
	if err != nil {
		hlog.Exit("数据库链接失败，请检查配置是否正确", err)
	}
	maxIdle := 8
	if nil != config.MaxIdle {
		maxIdle = *config.MaxIdle
	}
	db.SetMaxIdleConns(maxIdle)

	maxOpen := 16
	if nil != config.MaxActive {
		maxOpen = *config.MaxActive
	}
	db.SetMaxOpenConns(maxOpen)

	idleTimeout := time.Millisecond * 100
	if nil != config.IdleTimeout {
		idleTimeout = time.Millisecond * time.Duration(*config.IdleTimeout)
	}
	db.SetConnMaxIdleTime(idleTimeout)

	connMaxLifetime := time.Millisecond * 20000
	if nil != config.ConnMaxLifetime {
		connMaxLifetime = time.Millisecond * time.Duration(*config.ConnMaxLifetime)
	}
	db.SetConnMaxLifetime(connMaxLifetime)

	if nil != d.db {
		d.db.DB.Close()
	}
	d.db = &DB{DB: db, config: *config}
}

func (d *Database) Ping() error {
	return d.db.DB.Ping()
}

func NewDB() *Database {
	ret := &Database{}
	return ret
}

type Tx struct {
	t   *sql.Tx
	val *Context
}

func (t *Tx) Commit() error {
	if nil != t.t {
		err := t.t.Commit()
		t.t = nil
		t.val.tx = nil
		if err != nil {
			return erro.Wrap(err)
		}
	}
	return nil
}

func (t *Tx) Rollback() error {
	if nil != t.t {
		err := t.t.Rollback()
		t.t = nil
		t.val.tx = nil
		if err != nil {
			return erro.Wrap(err)
		}
	}
	return nil
}

func Begin(ctx context.Context) (*Tx, error) {
	var ret = ctx.Value(reflect.TypeOf(&Context{}))
	if nil == ret {
		return nil, erro.NewError("")
	}
	val := ret.(*Context)
	tx := &Tx{
		val: val,
	}
	if nil != val.tx {
		return tx, nil
	}
	val.tx = tx
	var err error
	tx.t, err = val.db.DB.Begin()
	if nil != err {
		return nil, erro.Wrap(err)
	}
	return tx, nil
}
