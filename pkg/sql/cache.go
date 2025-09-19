package sql

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"time"
)

type DbCache interface {
	Lock(ctx context.Context, table string, sql string) error
	Unlock(ctx context.Context, table string, sql string) error
	Get(ctx context.Context, table string, sql string, out any) (bool, error)
	Set(ctx context.Context, table string, sql string, in any, expiration time.Duration) error
	Del(ctx context.Context, table string) error
}

func NewCache(table string, builder *Builder) *Cache {
	return &Cache{
		table: table,
		sql:   builder.ToText(),
	}
}

type Cache struct {
	sql   string
	table string
}

func (c *Cache) Save(ctx context.Context, val any, fn func(ctx context.Context, val any) error) error {
	db, ok := FromContext(ctx)
	if !ok {
		return erro.NewError("no db in context")
	}
	if db.cache == nil {
		return fn(ctx, val)
	}

	ok, err := db.cache.Get(ctx, c.table, c.sql, val)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	err = db.cache.Lock(ctx, c.table, c.sql)
	if err != nil {
		return err
	}
	defer db.cache.Unlock(ctx, c.table, c.sql)

	ok, err = db.cache.Get(ctx, c.table, c.sql, val)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	err = fn(ctx, val)
	if err != nil {
		return err
	}

	err = db.cache.Set(ctx, c.table, c.sql, val, 0)
	if err != nil {
		return err
	}
	return nil
}
func (c *Cache) Clear(ctx context.Context) error {
	db, ok := FromContext(ctx)
	if !ok {
		return erro.NewError("no db in context")
	}
	if db.cache == nil {
		return nil
	}
	return db.cache.Del(ctx, c.table)
}
