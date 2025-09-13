package lock

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"sync"
	"sync/atomic"
	"time"
)

// lockMap 一个以字符串为键的 localLocks 映射。
var lockMap = make(map[string]*localLock)
var lock sync.Mutex

// 定时清理 lockMap，删除 count 为 0 的 localLocks。
func init() {
	ticker := time.NewTicker(time.Second * 30)
	go func() {
		for {
			<-ticker.C
			lock.Lock()
			for key, item := range lockMap {
				if item.count.Load() == 0 {
					delete(lockMap, key)
				}
			}
			lock.Unlock()
		}
	}()
}

// NewLocalLock 创建一个新的 localLock 并返回。
func NewLocalLock(key string) Locker {
	lock.Lock()
	defer lock.Unlock()
	ret, ok := lockMap[key]
	if ok {
		return ret
	}
	ret = &localLock{}
	lockMap[key] = ret
	return ret
}

// LocalLock 防止死锁，并确保同一时间只允许一个 goroutine 访问某个 key 的资源。
// 加锁后，会将 key 存入 context，以便在子函数中判断是否已经加锁。
func LocalLock(ctx context.Context, key string, f func(ctx context.Context) error) error {
	if key == "" {
		return erro.NewError("key is empty")
	}
	if nil == ctx {
		return erro.NewError("ctx is nil")
	}

	if nil != ctx.Value("key_lock_key:"+key) {
		return f(ctx)
	}
	ctx = context.WithValue(ctx, "key_lock_key:"+key, true)

	ret := NewLocalLock(key)
	ret.Lock()
	defer ret.Unlock()

	err := f(ctx)
	if err != nil {
		return err
	}
	return nil
}

// localLock 是一个可重入锁。
type localLock struct {
	lock  sync.Mutex
	count atomic.Int64
}

// Lock 加锁。
func (k *localLock) Lock() {
	k.count.Add(1)
	k.lock.Lock()
}

// Unlock 解锁。
func (k *localLock) Unlock() {
	k.lock.Unlock()
	k.count.Add(-1)
}

// TryLock 尝试加锁。
func (k *localLock) TryLock() bool {
	if k.lock.TryLock() {
		return true
	}
	k.count.Add(-1)
	return false
}

// LocalLockFallback 带有 fallback 函数的本地锁。
func LocalLockFallback(ctx context.Context, key string, primary func(ctx context.Context) (bool, error), fallback func(ctx context.Context) error) error {
	ret, err := primary(ctx)
	if err != nil {
		return err
	}
	if ret {
		return nil
	}

	err = LocalLock(ctx, key, fallback)
	if err != nil {
		return err
	}
	return nil
}
