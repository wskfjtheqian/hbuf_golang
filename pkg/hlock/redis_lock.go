package hlock

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// Lua：加锁
var lockScript = redis.NewScript(`
if redis.call("SET", KEYS[1], ARGV[1], "NX", "PX", ARGV[2]) then
	return 1
else
	return 0
end
`)

// Lua：解锁
var unlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
else
	return 0
end
`)

func New(client *redis.Client, key string, ttl time.Duration) *FastLock {
	return &FastLock{
		client: client,
		key:    key,
		value:  uuid.NewString(),
		ttl:    ttl,
	}
}

type FastLock struct {
	client *redis.Client
	key    string
	value  string
	ttl    time.Duration
}

func (l *FastLock) Lock(ctx context.Context, maxRetry int) bool {
	backoff := 10 * time.Microsecond

	for i := 0; i < maxRetry; i++ {
		res, err := lockScript.Run(ctx, l.client,
			[]string{l.key},
			l.value,
			l.ttl.Milliseconds(),
		).Int()

		if err == nil && res == 1 {
			return true
		}

		// 指数退避 + 随机抖动（避免惊群）
		sleep := backoff + time.Duration(rand.Int63n(int64(backoff)))
		time.Sleep(sleep)

		if backoff < time.Millisecond {
			backoff *= 2
		}

		select {
		case <-ctx.Done():
			return false
		default:
		}
	}

	return false
}

func (l *FastLock) Unlock(ctx context.Context) {
	_, _ = unlockScript.Run(ctx, l.client,
		[]string{l.key},
		l.value,
	).Result()
}

type KeyedLock struct {
	m sync.Map
}

func (k *KeyedLock) Lock(key string) func() {
	val, _ := k.m.LoadOrStore(key, &sync.Mutex{})
	mu := val.(*sync.Mutex)

	mu.Lock()

	return func() {
		mu.Unlock()
		k.m.Delete(key)
	}
}
