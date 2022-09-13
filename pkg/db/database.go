package db

import (
	"context"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	utl "github.com/wskfjtheqian/hbuf_golang/pkg/utils"
	"log"
	"reflect"
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
			return nil, utl.Wrap(err)
		}
		return rows, nil
	}
	rows, err := v.db.Query(query, args...)
	if err != nil {
		return nil, utl.Wrap(err)
	}
	return rows, nil
}

func (v *contextValue) Exec(query string, args ...any) (sql.Result, error) {
	if nil != v.tx {
		exec, err := v.tx.t.Exec(query, args...)
		if err != nil {
			return nil, utl.Wrap(err)
		}
		return exec, nil
	}
	exec, err := v.db.Exec(query, args...)
	if err != nil {
		return nil, utl.Wrap(err)
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
	db *sql.DB
}

func (d *Database) OnFilter(ctx context.Context) (context.Context, error) {
	if nil == ctx.Value(cType) {
		ctx = &Context{
			ctx,
			&contextValue{
				db: d.db,
			},
		}
	}
	return ctx, nil
}

func NewDB(con *Config) *Database {
	db, err := sql.Open(*con.Type, *con.Username+":"+*con.Password+"@"+*con.URL+"&parseTime=true&clientFoundRows=true")
	if err != nil {
		log.Fatalln("数据库链接失败，请检查配置是否正确", err)
	}
	maxIdle := 8
	if nil != con.MaxIdle {
		maxIdle = *con.MaxIdle
	}
	db.SetMaxIdleConns(maxIdle)

	maxOpen := 16
	if nil != con.MaxActive {
		maxOpen = *con.MaxActive
	}
	db.SetMaxOpenConns(maxOpen)

	idleTimeout := time.Millisecond * 100
	if nil != con.IdleTimeout {
		idleTimeout = time.Millisecond * time.Duration(*con.IdleTimeout)
	}
	db.SetConnMaxIdleTime(idleTimeout)
	return &Database{
		db: db,
	}
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
			return utl.Wrap(err)
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
			return utl.Wrap(err)
		}
	}
	return nil
}

func Begin(ctx context.Context) (*Tx, error) {
	var ret = ctx.Value(reflect.TypeOf(&Context{}))
	if nil == ret {
		return nil, utl.NewError("")
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
		return nil, utl.Wrap(err)
	}
	return tx, nil
}
