package hlock

import (
	"context"
	"hash/fnv"
	"sync"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

const shardCount = 128

type shard struct {
	mu sync.RWMutex
	m  map[string]*localLock
}

var shards [shardCount]*shard

func init() {
	for i := 0; i < shardCount; i++ {
		shards[i] = &shard{
			m: make(map[string]*localLock),
		}
	}
}

func getShard(key string) *shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return shards[h.Sum32()%shardCount]
}

// =========================
// 创建锁
// =========================

func NewLocalLock(key string) Locker {
	s := getShard(key)

	// fast path：读锁
	s.mu.RLock()
	if l, ok := s.m[key]; ok {
		s.mu.RUnlock()
		return l
	}
	s.mu.RUnlock()

	// slow path：写锁
	s.mu.Lock()
	defer s.mu.Unlock()

	// double check
	if l, ok := s.m[key]; ok {
		return l
	}

	l := &localLock{
		key:   key,
		shard: s,
	}

	s.m[key] = l
	return l
}

// =========================
// 锁实现
// =========================

type localLock struct {
	mu    sync.Mutex
	key   string
	shard *shard
}

// =========================
// Lock
// =========================

func (l *localLock) Lock(ctx context.Context) error {
	l.mu.Lock()
	return nil
}

// =========================
// Unlock（自动删除）
// =========================

func (l *localLock) Unlock(ctx context.Context) error {
	l.mu.Unlock()
	s := l.shard

	// 删除必须加写锁
	s.mu.Lock()
	defer s.mu.Unlock()

	// double check（防止误删新锁）
	if cur, ok := s.m[l.key]; ok && cur == l {
		delete(s.m, l.key)
	}

	return nil
}

// =========================
// TryLock
// =========================

func (l *localLock) TryLock(ctx context.Context) (bool, error) {

	if l.mu.TryLock() {
		return true, nil
	}
	return false, nil
}

// WithLocalGetOrPopulate 带有 fallback 函数的分布式控制系统加锁, 基于ctx的可重入锁。
func WithLocalGetOrPopulate[T any](ctx context.Context, key string, get func(ctx context.Context) (T, bool, error), populate func(ctx context.Context) (T, error)) (T, error) {
	val, ret, err := get(ctx)
	if err != nil {
		return val, err
	}
	if ret {
		return val, nil
	}

	ctx, l := localContext(ctx, key)
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

// WithLocal 分布式控制系统加锁, 基于ctx的可重入锁。
func WithLocal[T any](ctx context.Context, key string, fun func(ctx context.Context) (T, error)) (T, error) {
	var val T
	ctx, l := localContext(ctx, key)
	err := l.Lock(ctx)
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

// WithLocalTry 分布式控制系统尝试加锁, 基于ctx的可重入锁。
func WithLocalTry[T any](ctx context.Context, key string, fun func(ctx context.Context) (T, error)) (T, error) {
	var val T
	ctx, l := localContext(ctx, key)
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

func localContext(ctx context.Context, key string) (context.Context, Locker) {
	key = "local_lock_" + key
	value := ctx.Value(key)
	if value != nil {
		return ctx, value.(*localLock)
	}

	l := NewLocalLock(key)
	ctx = context.WithValue(ctx, key, l)
	return ctx, l
}
