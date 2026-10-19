package hsql

import (
	"context"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcache"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

func SaveCache[T any](ctx context.Context, schema, table string, builder *Builder, expiration time.Duration, fn func(ctx context.Context) (T, error)) (T, error) {
	var val T
	db, ok := FromContext(ctx)
	if !ok {
		return val, herror.NewError("no db in context")
	}
	if db.GetCache() == nil {
		return fn(ctx)
	}

	sql := builder.ToText(ctx)
	key := hutl.Md5([]byte(sql))
	return hcache.SaveCache(ctx, db.GetCache(), schema+"."+table, key, expiration, fn)

}

func ClearCache(ctx context.Context, schema, table string) error {
	db, ok := FromContext(ctx)
	if !ok {
		return herror.NewError("no db in context")
	}
	if db.GetCache() == nil {
		return nil
	}
	return db.GetCache().Del(ctx, schema+"."+table)
}
