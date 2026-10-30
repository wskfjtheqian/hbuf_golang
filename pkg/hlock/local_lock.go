package hlock

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

const shardCount = 128

// lockPollInterval Lock 等待锁期间的轮询间隔。
const lockPollInterval = 10 * time.Millisecond

// DefaultLocalLockTimeout 组合锁（localEtcd/localRedis）中本地锁的默认等待超时，
// 防止 ctx 无 deadline 时本地锁永久阻塞。
var DefaultLocalLockTimeout = 30 * time.Second

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

func fnv32a(s string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime32
	}
	return h
}

func getShard(key string) *shard {
	return shards[fnv32a(key)%shardCount]
}

// goroutineID 获取当前 goroutine 的 id，用作可重入锁的持有者标识。
func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// buf 形如 "goroutine 123 [running]:..."
	fields := strings.Fields(string(buf[:n]))
	if len(fields) < 2 {
		return 0
	}
	id, _ := strconv.ParseUint(fields[1], 10, 64)
	return id
}

// =========================
// 创建锁
// =========================

// LocalLockOption 本地锁选项。
type LocalLockOption func(l *localLock)

// WithLocalLockTimeout 设置 Lock 等待锁的最大时长。
// 与 ctx deadline 同时存在时取更早者生效，ctx 取消始终即时响应；
// 选项仅在锁实例首次创建时生效，后续 NewLocalLock 复用已有实例时忽略。
func WithLocalLockTimeout(timeout time.Duration) LocalLockOption {
	return func(l *localLock) {
		l.timeout.Store(int64(timeout))
	}
}

// NewLocalLock 创建或复用 key 对应的本地锁实例。
// 每获取一次实例引用计数 +1，调用方用完（含 TryLock 失败后放弃）应调用 Unlock 释放引用，
// 引用归零且实例仍在分片 map 中时才会被回收。
func NewLocalLock(key string, options ...LocalLockOption) Locker {
	s := getShard(key)

	// fast path：读锁
	s.mu.RLock()
	if l, ok := s.m[key]; ok {
		l.refs.Add(1)
		s.mu.RUnlock()
		return l
	}
	s.mu.RUnlock()

	// slow path：写锁
	s.mu.Lock()
	defer s.mu.Unlock()

	// double check
	if l, ok := s.m[key]; ok {
		l.refs.Add(1)
		return l
	}

	l := &localLock{
		key:   key,
		shard: s,
	}
	l.refs.Store(1)
	for _, option := range options {
		option(l)
	}

	s.m[key] = l
	return l
}

// =========================
// 锁实现
// =========================

type localLock struct {
	mu      sync.Mutex
	owner   atomic.Uint64 // 持有者 goroutine id，0 表示未持有
	reentry int           // 可重入深度，仅在持有 mu 时读写
	refs    atomic.Int32  // 引用计数，控制实例在分片 map 中的回收时机
	timeout atomic.Int64  // Lock 等待锁的最大时长（纳秒），0 表示无超时，仅依赖 ctx
	key     string
	shard   *shard
}

// tryAcquire 尝试加锁。同一 goroutine 重复获取时走可重入路径。
func (l *localLock) tryAcquire() bool {
	id := goroutineID()
	if l.owner.Load() == id {
		l.reentry++
		return true
	}
	if l.mu.TryLock() {
		l.owner.Store(id)
		l.reentry = 1
		return true
	}
	return false
}

// =========================
// Lock（支持 ctx 超时/取消）
// =========================

func (l *localLock) Lock(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if l.tryAcquire() {
		return nil
	}

	// 锁实例超时与 ctx deadline 取更早者生效，ctx 取消始终即时响应
	if timeout := time.Duration(l.timeout.Load()); timeout > 0 {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > timeout {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
	}

	ticker := time.NewTicker(lockPollInterval)
	defer ticker.Stop()

	for {
		if l.tryAcquire() {
			return nil
		}
		select {
		case <-ctx.Done():
			// 加锁失败，释放本次获取的引用
			l.release()
			return herror.Wrap(ctx.Err())
		case <-ticker.C:
		}
	}
}

// =========================
// Unlock（自动回收）
// =========================

// Unlock 解锁并释放一次引用。未持有锁或持有者不是当前 goroutine 时解锁动作被忽略，
// 不会 panic；引用计数归零且锁空闲时回收实例。
func (l *localLock) Unlock(ctx context.Context) error {
	id := goroutineID()
	if l.owner.Load() == id {
		l.reentry--
		if l.reentry == 0 {
			l.owner.Store(0)
			l.mu.Unlock()
		}
	}

	l.release()
	return nil
}

// release 释放一次引用。引用归零且锁空闲时，在分片写锁下复查后回收实例，
// 避免与并发的新获取竞争导致误删仍被引用或持有的实例。
func (l *localLock) release() {
	for {
		n := l.refs.Load()
		if n <= 0 {
			break
		}
		if l.refs.CompareAndSwap(n, n-1) {
			break
		}
	}

	s := l.shard
	s.mu.Lock()
	defer s.mu.Unlock()

	if l.refs.Load() <= 0 && l.owner.Load() == 0 {
		// double check（防止误删新锁）
		if cur, ok := s.m[l.key]; ok && cur == l {
			delete(s.m, l.key)
		}
	}
}

// =========================
// TryLock
// =========================

func (l *localLock) TryLock(ctx context.Context) (bool, error) {
	if l.tryAcquire() {
		return true, nil
	}
	return false, nil
}

// WithLocalGetOrPopulate 带有 fallback 函数的本地加锁, 基于 goroutine 的可重入锁。
func WithLocalGetOrPopulate[T any](ctx context.Context, key string, get func(ctx context.Context) (T, bool, error), populate func(ctx context.Context) (T, error)) (T, error) {
	val, ret, err := get(ctx)
	if err != nil {
		return val, err
	}
	if ret {
		return val, nil
	}

	l := NewLocalLock(key)
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

// WithLocal 本地加锁, 基于 goroutine 的可重入锁。
func WithLocal[T any](ctx context.Context, key string, fun func(ctx context.Context) (T, error)) (T, error) {
	var val T
	l := NewLocalLock(key)
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

// WithLocalTry 本地尝试加锁, 基于 goroutine 的可重入锁。
func WithLocalTry[T any](ctx context.Context, key string, fun func(ctx context.Context) (T, error)) (T, error) {
	var val T
	l := NewLocalLock(key)
	lock, err := l.TryLock(ctx)
	if err != nil {
		return val, err
	}
	if !lock {
		// 加锁失败且不再使用，释放引用避免实例驻留分片 map
		_ = l.Unlock(ctx)
		return val, herror.NewError("lock failed")
	}
	defer l.Unlock(ctx)

	val, err = fun(ctx)
	if err != nil {
		return val, err
	}
	return val, nil
}
