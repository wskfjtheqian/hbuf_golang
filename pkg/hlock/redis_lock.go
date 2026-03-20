package hlock

import (
	"context"
	"math/rand"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hredis"
)

type redisLock struct {
	redis *hredis.Redis
	key   string
	value string
	ttl   time.Duration
}

// Lua 解锁（必须）
var unlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
else
	return 0
end
`)

type RedisLockOption func(l *redisLock)

func WithRedisLockTTL(duration time.Duration) RedisLockOption {
	return func(l *redisLock) {
		l.ttl = duration
	}
}

func NewRedisLock(ctx context.Context, key string, options ...RedisLockOption) (Locker, error) {
	client, ok := hredis.FromContext(ctx)
	if !ok {
		return nil, herror.NewError("redis not found in context")
	}
	ret := &redisLock{
		redis: client,
		key:   key,
		value: uuid.NewString(),
		ttl:   time.Second * 10,
	}
	for _, option := range options {
		option(ret)
	}

	return ret, nil
}

// TryLock （无 Lua）
func (l *redisLock) TryLock(ctx context.Context) (bool, error) {
	ok, err := l.redis.Get().SetNX(ctx, l.key, l.value, l.ttl).Result()
	if err != nil {
		return false, herror.Wrap(err)
	}
	return ok, nil
}

// Lock （指数退避 + 抖动）
func (l *redisLock) Lock(ctx context.Context) error {
	backoff := 10 * time.Microsecond

	for {
		lock, err := l.TryLock(ctx)
		if err != nil {
			return err
		}
		if lock {
			l.startWatchdog(ctx)
			return nil
		}

		sleep := backoff + time.Duration(rand.Int63n(int64(backoff)))
		time.Sleep(sleep)

		if backoff < time.Millisecond {
			backoff *= 2
		}
	}
}

// Unlock （优化：GET + Lua）
func (l *redisLock) Unlock(ctx context.Context) error {
	client := l.redis.Get()

	val, err := client.Get(ctx, l.key).Result()
	if err != nil {
		return herror.Wrap(err)
	}
	if val != l.value {
		return nil
	}

	_, err = unlockScript.Run(
		ctx,
		client,
		[]string{l.key},
		l.value,
	).Result()
	if err != nil {
		return err
	}
	return nil
}

// watchdog 自动续期
func (l *redisLock) startWatchdog(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(l.ttl / 2)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				client := l.redis.Get()
				val, err := client.Get(ctx, l.key).Result()
				if err != nil || val != l.value {
					return
				}
				client.Expire(ctx, l.key, l.ttl)
			case <-ctx.Done():
				return
			}
		}
	}()
}

type localRedis struct {
	local Locker
	redis Locker
}

func (l localRedis) Lock(ctx context.Context) error {
	_ = l.local.Lock(ctx)
	return l.redis.Lock(ctx)
}

func (l localRedis) Unlock(ctx context.Context) error {
	_ = l.redis.Unlock(ctx)
	_ = l.local.Unlock(ctx)
	return nil
}

func (l localRedis) TryLock(ctx context.Context) (bool, error) {
	lock, _ := l.local.TryLock(ctx)
	if lock {
		return l.redis.TryLock(ctx)
	}
	return false, nil
}

// WithRedisGetOrPopulate 带有 fallback 函数的分布式控制系统加锁, 基于ctx的可重入锁,优先走本地锁。
func WithRedisGetOrPopulate[T any](ctx context.Context, key string, get func(ctx context.Context) (T, bool, error), populate func(ctx context.Context) (T, error), options ...RedisLockOption) (T, error) {
	val, ret, err := get(ctx)
	if err != nil {
		return val, err
	}
	if ret {
		return val, nil
	}

	ctx, l, err := redisContext(ctx, key, options...)
	if err != nil {
		return val, err
	}
	err = l.Lock(ctx)
	if err != nil {
		return val, err
	}
	defer l.Unlock(ctx)

	val, ret, err = get(ctx)
	if err != nil {
		return val, err
	}
	if ret {
		return val, nil
	}

	val, err = populate(ctx)
	if err != nil {
		return val, err
	}
	return val, nil
}

// WithRedis 分布式控制系统加锁, 基于ctx的可重入锁,优先走本地锁。
func WithRedis[T any](ctx context.Context, key string, fun func(ctx context.Context) (T, error), options ...RedisLockOption) (T, error) {
	var val T
	ctx, l, err := redisContext(ctx, key, options...)
	if err != nil {
		return val, err
	}
	err = l.Lock(ctx)
	if err != nil {
		return val, err
	}
	defer l.Unlock(ctx)

	val, err = fun(ctx)
	if err != nil {
		return val, err
	}
	return val, nil
}

// WithRedisTry 分布式控制系统尝试加锁, 基于ctx的可重入锁,优先走本地锁。
func WithRedisTry[T any](ctx context.Context, key string, fun func(ctx context.Context) (T, error), options ...RedisLockOption) (T, error) {
	var val T
	ctx, l, err := redisContext(ctx, key, options...)
	if err != nil {
		return val, err
	}
	lock, err := l.TryLock(ctx)
	if err != nil {
		return val, err
	}
	if !lock {
		return val, herror.NewError("lock failed")
	}
	defer l.Unlock(ctx)

	val, err = fun(ctx)
	if err != nil {
		return val, err
	}
	return val, nil
}

func redisContext(ctx context.Context, key string, options ...RedisLockOption) (context.Context, Locker, error) {
	key = "locker:" + key
	value := ctx.Value(key)
	if value != nil {
		return ctx, value.(*redisLock), nil
	}

	lock, err := NewRedisLock(ctx, key, options...)
	if err != nil {
		return nil, nil, err
	}
	l := &localRedis{
		local: NewLocalLock(key),
		redis: lock,
	}

	ctx = context.WithValue(ctx, key, l)
	return ctx, l, nil
}
