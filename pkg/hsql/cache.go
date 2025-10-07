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
	if db.cache == nil {
		_, err = fn(ctx)
		return err
	}
	table = *db.config.DbName + "." + table
	sql := builder.ToText()
	key := table + ":" + hutl.Md5([]byte(sql))

	ok, err = db.cache.Get(ctx, key, table, val, expiration)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	err = db.cache.Lock(ctx, key)
	if err != nil {
		return err
	}
	defer db.cache.Unlock(ctx, key)

	ok, err = db.cache.Get(ctx, key, table, val, expiration)
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

	err = db.cache.Set(ctx, key, table, sql, val, expiration)
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
	if db.cache == nil {
		return nil
	}
	return db.cache.Del(ctx, table)
}
