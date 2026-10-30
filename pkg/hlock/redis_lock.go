package hlock

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hredis"
)

type redisLock struct {
	redis   *hredis.Redis
	key     string
	value   string
	ttl     time.Duration
	timeout time.Duration

	watchdogMu     sync.Mutex
	watchdogCancel context.CancelFunc // watchdog 停止句柄，Unlock 时主动取消
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

// WithRedisLockTTL 设置锁 key 的过期时间，持有期间由 watchdog 自动续期。
func WithRedisLockTTL(duration time.Duration) RedisLockOption {
	return func(l *redisLock) {
		l.ttl = duration
	}
}

// WithRedisLockTimeout 设置 Lock 等待锁的最大时长。
// 与 ctx deadline 同时存在时取更早者生效，ctx 取消始终即时响应；
// 组合锁（WithRedis* 系列）中的本地锁超时保持与其一致。
func WithRedisLockTimeout(timeout time.Duration) RedisLockOption {
	return func(l *redisLock) {
		l.timeout = timeout
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

// Lock （指数退避 + 抖动，支持 ctx 取消/超时）
func (l *redisLock) Lock(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 等待锁用超时 ctx（与 ctx deadline 取更早者），watchdog 用原始 ctx 保持续期
	waitCtx := ctx
	if l.timeout > 0 {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > l.timeout {
			var cancel context.CancelFunc
			waitCtx, cancel = context.WithTimeout(ctx, l.timeout)
			defer cancel()
		}
	}

	backoff := 10 * time.Microsecond

	for {
		lock, err := l.TryLock(waitCtx)
		if err != nil {
			return err
		}
		if lock {
			l.startWatchdog(ctx)
			return nil
		}

		sleep := backoff + time.Duration(rand.Int63n(int64(backoff)))
		timer := time.NewTimer(sleep)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return herror.Wrap(waitCtx.Err())
		case <-timer.C:
		}

		if backoff < time.Millisecond {
			backoff *= 2
		}
	}
}

// Unlock （优化：GET + Lua）
func (l *redisLock) Unlock(ctx context.Context) error {
	// 先停止 watchdog，避免解锁后残留的续期 goroutine 访问已关闭的连接
	l.watchdogMu.Lock()
	if l.watchdogCancel != nil {
		l.watchdogCancel()
		l.watchdogCancel = nil
	}
	l.watchdogMu.Unlock()

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
	l.watchdogMu.Lock()
	defer l.watchdogMu.Unlock()

	if l.watchdogCancel != nil {
		l.watchdogCancel()
	}
	wctx, cancel := context.WithCancel(ctx)
	l.watchdogCancel = cancel

	go func() {
		ticker := time.NewTicker(l.ttl / 2)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				client := l.redis.Get()
				if client == nil {
					return
				}
				val, err := client.Get(wctx, l.key).Result()
				if err != nil || val != l.value {
					return
				}
				client.Expire(wctx, l.key, l.ttl)
			case <-wctx.Done():
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
	if !lock {
		// 本地锁失败，释放本地锁实例引用
		_ = l.local.Unlock(ctx)
		return false, nil
	}

	lock, err := l.redis.TryLock(ctx)
	if !lock || err != nil {
		// redis 加锁失败，释放已持有的本地锁
		_ = l.local.Unlock(ctx)
	}
	return lock, err
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
		return ctx, value.(Locker), nil
	}

	lock, err := NewRedisLock(ctx, key, options...)
	if err != nil {
		return nil, nil, err
	}

	// 本地锁超时与 redis 锁超时保持一致，未显式设置时用默认值
	timeout := DefaultLocalLockTimeout
	if rl, ok := lock.(*redisLock); ok && rl.timeout > 0 {
		timeout = rl.timeout
	}
	l := &localRedis{
		local: NewLocalLock(key, WithLocalLockTimeout(timeout)),
		redis: lock,
	}

	ctx = context.WithValue(ctx, key, l)
	return ctx, l, nil
}
