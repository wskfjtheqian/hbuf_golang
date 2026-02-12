package hcache

import (
	"context"
	"time"
)

type Cache interface {
	Lock(ctx context.Context, key string) error
	Unlock(ctx context.Context, key string) error
	Get(ctx context.Context, key string, group string, out any, expiration time.Duration) (bool, error)
	Set(ctx context.Context, key string, group string, text string, in any, expiration time.Duration) error
	Del(ctx context.Context, group string) error
}

func SaveCache(ctx context.Context, cache Cache, group string, key string, val any, expiration time.Duration, fn func(ctx context.Context) (any, error)) error {
	ok, err := cache.Get(ctx, key, group, val, expiration)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	err = cache.Lock(ctx, group+":"+key)
	if err != nil {
		return err
	}
	defer cache.Unlock(ctx, group+":"+key)

	ok, err = cache.Get(ctx, key, group, val, expiration)
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

	err = cache.Set(ctx, key, group, key, val, expiration)
	if err != nil {
		return err
	}
	return nil
}
