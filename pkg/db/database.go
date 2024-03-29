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
}

type contextValue struct {
	db *sql.DB
	tx *Tx
}

func (v *contextValue) Query(query string, args ...any) (*sql.Rows, error) {
	if nil != v.tx {
		rows, err := v.tx.t.Query(query, args...)
		if err != nil {
			return nil, erro.Wrap(err)
		}
		return rows, nil
	}
	rows, err := v.db.Query(query, args...)
	if err != nil {
		return nil, erro.Wrap(err)
	}
	return rows, nil
}

func (v *contextValue) Exec(query string, args ...any) (sql.Result, error) {
	if nil != v.tx {
		exec, err := v.tx.t.Exec(query, args...)
		if err != nil {
			return nil, erro.Wrap(err)
		}
		return exec, nil
	}
	exec, err := v.db.Exec(query, args...)
	if err != nil {
		return nil, erro.Wrap(err)
	}
	return exec, nil
}

type Context struct {
	context.Context
	value *contextValue
}

var cType = reflect.TypeOf(&Context{})

func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d.value
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
	return ret.(*contextValue)
}

type Database struct {
	db     *sql.DB
	config *Config
	lock   sync.Mutex
}

func (d *Database) OnFilter(ctx context.Context, data hbuf.Data, in *rpc.Filter, call rpc.FilterCall) (context.Context, hbuf.Data, error) {
	if nil == ctx.Value(cType) {
		ctx = &Context{
			ctx,
			&contextValue{
				db: d.db,
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
			d.db.Close()
		}
		d.db = nil
		d.config = nil
		return
	}
	if nil != d.config && d.config.Yaml() == config.Yaml() {
		return
	}
	d.config = config

	db, err := sql.Open(*config.Type, *config.Username+":"+*config.Password+"@"+*config.URL+"&parseTime=true&clientFoundRows=true")
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
	if nil != d.db {
		d.db.Close()
	}
	d.db = db
}

func (d *Database) Ping() error {
	return d.db.Ping()
}

func NewDB() *Database {
	ret := &Database{}
	return ret
}

type Tx struct {
	t   *sql.Tx
	val *contextValue
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
	val := ret.(*contextValue)
	tx := &Tx{
		val: val,
	}
	if nil != val.tx {
		return tx, nil
	}
	val.tx = tx
	var err error
	tx.t, err = val.db.Begin()
	if nil != err {
		return nil, erro.Wrap(err)
	}
	return tx, nil
}
