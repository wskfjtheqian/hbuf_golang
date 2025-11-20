package hsql

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
	"time"
)

type DbCache interface {
	Lock(ctx context.Context, key string) error
	Unlock(ctx context.Context, key string) error
	Get(ctx context.Context, key string, table string, out any, expiration time.Duration) (bool, error)
	Set(ctx context.Context, key string, table string, sql string, in any, expiration time.Duration) error
	Del(ctx context.Context, table string) error
}

func SaveCache(ctx context.Context, table string, builder *Builder, val any, expiration time.Duration, fn func(ctx context.Context) (any, error)) error {
	db, ok := FromContext(ctx)
	if !ok {
		return herror.NewError("no db in context")
	}
	var err error
	if db.GetCache() == nil {
		_, err = fn(ctx)
		return err
	}
	table = *db.GetConfig().DbName + "." + table
	sql := builder.ToText()
	key := hutl.Md5([]byte(sql))

	ok, err = db.GetCache().Get(ctx, key, table, val, expiration)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	err = db.GetCache().Lock(ctx, table+":"+key)
	if err != nil {
		return err
	}
	defer db.GetCache().Unlock(ctx, table+":"+key)

	ok, err = db.GetCache().Get(ctx, key, table, val, expiration)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	val, err = fn(ctx)
	if err != nil {
		return err
	}

	err = db.GetCache().Set(ctx, key, table, sql, val, expiration)
	if err != nil {
		return err
	}
	return nil
}

func ClearCache(ctx context.Context, table string) error {
	db, ok := FromContext(ctx)
	if !ok {
		return herror.NewError("no db in context")
	}
	if db.GetCache() == nil {
		return nil
	}
	table = *db.GetConfig().DbName + "." + table

	return db.GetCache().Del(ctx, table)
}
