package redis

import (
	"context"
	"encoding/json"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/utl"
	"time"
)

type DBCache struct {
}

func (d *DBCache) Get(ctx context.Context, table string, sql string, out any) (bool, error) {
	r, ok := FromContext(ctx)
	if !ok {
		return false, erro.NewError("redis not found in context")
	}
	key := "db:cache:" + table + ":" + utl.Md5(sql)

	cmd := r.Get().Get(ctx, key)
	if cmd.Err() != nil {
		return false, erro.Wrap(cmd.Err())
	}

	if cmd.Val() == "" {
		return true, nil
	}
	err := json.Unmarshal([]byte(cmd.Val()), out)
	if err != nil {
		return false, erro.Wrap(err)
	}
	return true, nil
}

func (d *DBCache) Set(ctx context.Context, table string, sql string, in any, expiration time.Duration) error {
	r, ok := FromContext(ctx)
	if !ok {
		return erro.NewError("redis not found in context")
	}
	bytes, err := json.Marshal(in)
	if err != nil {
		return erro.Wrap(err)
	}
	c := r.Get()
	key := "db:cache:" + table
	reply := c.Set(ctx, key+":"+utl.Md5(sql), bytes, expiration)
	if reply.Err() != nil {
		return erro.Wrap(reply.Err())
	}
	c.HSet(ctx, key+":"+table, utl.Md5(reply.Val()), bytes)
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
