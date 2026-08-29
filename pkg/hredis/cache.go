package hredis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcache"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hgo"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

func NewCache(prefix string) hcache.Cache {
	return &DBCache{prefix: prefix}
}

type DBCache struct {
	prefix string
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
		return false, herror.NewError("redis not found in context")
	}

	c := r.Get()
	key = d.prefix + ":cache:" + table + ":" + key
	cmd := c.Get(ctx, key)

	err := cmd.Err()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, herror.Wrap(err)
	}
	if cmd.Val() == "null" {
		return true, nil
	}

	err = json.Unmarshal([]byte(cmd.Val()), out)
	if err != nil {
		d.Del(ctx, table)
		herror.PrintStack(ctx, err)
		return false, nil
	}

	c.Expire(ctx, key, expiration)
	return true, nil
}

func (d *DBCache) Set(ctx context.Context, key string, table string, sql string, in any, expiration time.Duration) error {
	r, ok := FromContext(ctx)
	if !ok {
		return herror.NewError("redis not found in context")
	}
	bytes, err := json.Marshal(in)
	if err != nil {
		return herror.Wrap(err)
	}

	c := r.Get()
	reply := c.Set(ctx, d.prefix+":cache:"+table+":"+key, bytes, expiration)
	if err = reply.Err(); err != nil {
		return herror.Wrap(err)
	}

	c.HSet(ctx, d.prefix+":cache:"+table, key, nil)
	c.HSet(ctx, d.prefix+":cache", table, nil)
	return nil
}

func (d *DBCache) Del(ctx context.Context, table string) error {
	r, ok := FromContext(ctx)
	if !ok {
		return herror.NewError("redis not found in context")
	}

	hgo.Go(ctx, func(ctx context.Context) error {
		time.Sleep(time.Second * 1)
		return d.del(context.Background(), table, r.Get())
	})
	return d.del(ctx, table, r.Get())
}

func (d *DBCache) del(ctx context.Context, table string, c *redis.Client) error {
	key := d.prefix + ":cache:" + table

	reply := c.HGetAll(ctx, key)
	if err := reply.Err(); err != nil {
		return herror.Wrap(err)
	}

	keys := hutl.Slice(hutl.Keys(reply.Val()), func(i int, v string) string {
		return key + ":" + v
	})
	keys = append(keys, key)

	err := c.Del(ctx, keys...).Err()
	if err != nil {
		return herror.Wrap(err)
	}
	return nil
}

// ClearExpired 清除过期缓存Key
func ClearExpired(ctx context.Context, prefix string) error {
	r, ok := FromContext(ctx)
	if !ok {
		return herror.NewError("redis not found in context")
	}
	c := r.Get()
	var cursor uint64
	for {
		var keys []string
		var err error
		keys, cursor, err = c.HScan(ctx, prefix+":cache", cursor, "*", 1000).Result()
		if err != nil {
			return herror.Wrap(err)
		}

		for i := 0; i < len(keys); i += 2 {
			err := clearTableExpired(ctx, c, keys[i], prefix)
			if err != nil {
				return err
			}
		}
		if cursor == 0 {
			break
		}
	}
	return nil
}

func clearTableExpired(ctx context.Context, c *redis.Client, key string, prefix string) error {
	delKeys := make([]string, 0)

	var cursor uint64
	for {
		var keys []string
		var err error
		keys, cursor, err = c.HScan(ctx, prefix+":cache:"+key, cursor, "*", 1000).Result()
		if err != nil {
			return herror.Wrap(err)
		}

		for i := 0; i < len(keys); i += 2 {
			subKey := keys[i]
			reply := c.Exists(ctx, prefix+":cache:"+subKey)
			if err := reply.Err(); err != nil {
				return herror.Wrap(err)
			}
			if reply.Val() == 0 {
				delKeys = append(delKeys, subKey)
			}
		}
		if cursor == 0 {
			break
		}
	}

	if len(delKeys) > 0 {
		err := c.HDel(ctx, key, delKeys...).Err()
		if err != nil {
			return herror.Wrap(err)
		}
	}
	return nil
}
