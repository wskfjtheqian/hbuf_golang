package redis

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-redis/redis/v8"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/sql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/utl"
	"time"
)

func NewDBCache() sql.DbCache {
	return &DBCache{}
}

type DBCache struct {
}

func (d *DBCache) Lock(ctx context.Context, key string) error {

	return nil
}
func (d *DBCache) Unlock(ctx context.Context, key string) error {

	return nil
}

func (d *DBCache) Get(ctx context.Context, key string, table string, out any, expiration time.Duration) (bool, error) {
	r, ok := FromContext(ctx)
	if !ok {
		return false, erro.NewError("redis not found in context")
	}
	c := r.Get()

	cmd := c.Get(ctx, "db:cache:"+key)
	if errors.Is(cmd.Err(), redis.Nil) {
		return false, nil
	}
	if cmd.Err() != nil {
		return false, erro.Wrap(cmd.Err())
	}
	if cmd.Val() == "null" {
		return true, nil
	}
	err := json.Unmarshal([]byte(cmd.Val()), out)
	if err != nil {
		return false, erro.Wrap(err)
	}

	c.Expire(ctx, "db:cache:"+key, expiration)
	return true, nil
}

func (d *DBCache) Set(ctx context.Context, key string, table string, sql string, in any, expiration time.Duration) error {
	r, ok := FromContext(ctx)
	if !ok {
		return erro.NewError("redis not found in context")
	}
	bytes, err := json.Marshal(in)
	if err != nil {
		return erro.Wrap(err)
	}
	c := r.Get()
	reply := c.Set(ctx, "db:cache:"+key, bytes, expiration)
	if reply.Err() != nil {
		return erro.Wrap(reply.Err())
	}
	c.HSet(ctx, "db:cache:"+table, key, nil)
	return nil
}

func (d *DBCache) Del(ctx context.Context, table string) error {
	r, ok := FromContext(ctx)
	if !ok {
		return erro.NewError("redis not found in context")
	}
	key := "db:cache:" + table
	c := r.Get()
	reply := c.HGetAll(ctx, key)
	if reply.Err() != nil {
		return erro.Wrap(reply.Err())
	}
	keys := append(utl.Keys(reply.Val()), key)
	keys = utl.Slice(keys, func(v string) string {
		return key + ":" + v
	})

	err := c.Del(ctx, keys...).Err()
	if err != nil {
		return erro.Wrap(err)
	}

	go func() {
		time.Sleep(1 * time.Second)
		err := c.Del(ctx, keys...).Err()
		if err != nil {
			hlog.Error("redis del error: %v", err)
		}
	}()
	return nil
}
