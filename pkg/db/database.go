package db

import (
	"context"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"log"
	"reflect"
	"time"
)

type contextValue struct {
	db    *sql.DB
	begin *sql.Tx
	stack int
}

type Context struct {
	context.Context
	value *contextValue
}

var cType = reflect.TypeOf(&Context{})

func (d *Context) Value(key interface{}) interface{} {
	if reflect.TypeOf(d) == key {
		return d.value
	}
	return d.Context.Value(key)
}

func (d *Context) Done() <-chan struct{} {
	return d.Context.Done()
}

func GET(ctx context.Context) *sql.DB {
	var ret = ctx.Value(cType)
	if nil == ret {
		return nil
	}
	return ret.(*contextValue).db
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

func Begin(ctx context.Context) error {
	var ret = ctx.Value(reflect.TypeOf(&Context{}))
	if nil == ret {
		return nil
	}
	val := ret.(*contextValue)
	if 0 != val.stack {
		return nil
	}
	var err error
	val.begin, err = val.db.Begin()
	val.stack++
	return err
}

func Commit(ctx context.Context) error {
	var ret = ctx.Value(reflect.TypeOf(&Context{}))
	if nil == ret || nil == ret.(*contextValue).begin {
		return nil
	}
	val := ret.(*contextValue)
	if 1 != val.stack {
		return nil
	}
	err := val.begin.Commit()
	if err != nil {
		return err
	}
	val.begin = nil
	return nil
}

func Rollback(ctx context.Context) {
	var ret = ctx.Value(reflect.TypeOf(&Context{}))
	if nil == ret {
		return
	}
	val := ret.(*contextValue)
	val.stack--
	if 0 != val.stack {
		return
	}
	if nil == ret.(*contextValue).begin {
		return
	}
	err := val.begin.Rollback()
	if err != nil {
		print(err)
	}
	val.begin = nil
}
