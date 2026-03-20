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

func SaveCache[T any](ctx context.Context, cache Cache, group string, key string, expiration time.Duration, fn func(ctx context.Context) (T, error)) (T, error) {
	if cache == nil {
		return fn(ctx)
	}

	var val T
	ok, err := cache.Get(ctx, key, group, &val, expiration)
	if err != nil {
		return val, err
	}
	if ok {
		return val, nil
	}

	err = cache.Lock(ctx, group+":"+key)
	if err != nil {
		return val, err
	}
	defer cache.Unlock(ctx, group+":"+key)

	ok, err = cache.Get(ctx, key, group, &val, expiration)
	if err != nil {
		return val, err
	}
	if ok {
		return val, nil
	}

	val, err = fn(ctx)
	if err != nil {
		return val, err
	}

	err = cache.Set(ctx, key, group, key, val, expiration)
	if err != nil {
		return val, err
	}
	return val, nil
}

func DelCache[T any](ctx context.Context, cache Cache, group string, fu func(ctx context.Context) (T, error)) (T, error) {
	if cache == nil {
		return fu(ctx)
	}

	var ret T
	err := cache.Del(ctx, group)
	if err != nil {
		return ret, err
	}

	ret, err = fu(ctx)
	if err != nil {
		return ret, err
	}

	_ = cache.Del(ctx, group)
	return ret, nil
}
