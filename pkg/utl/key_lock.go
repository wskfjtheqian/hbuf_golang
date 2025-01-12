package utl

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"sync"
	"sync/atomic"
	"time"
)

// Locker 是一个可重入锁。
type Locker interface {
	Lock()
	Unlock()
	TryLock() bool
}

// lockMap 一个以字符串为键的 keyLocks 映射。
var lockMap = make(map[string]*keyLock)
var lock sync.Mutex

// 定时清理 lockMap，删除 count 为 0 的 keyLocks。
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

// NewKeyLock 创建一个新的 keyLock 并返回。
func NewKeyLock(key string) Locker {
	lock.Lock()
	defer lock.Unlock()
	ret, ok := lockMap[key]
	if ok {
		return ret
	}
	ret = &keyLock{}
	lockMap[key] = ret
	return ret
}

// KeyLock 防止死锁，并确保同一时间只允许一个 goroutine 访问某个 key 的资源。
// 加锁后，会将 key 存入 context，以便在子函数中判断是否已经加锁。
func KeyLock(ctx context.Context, key string, f func(ctx context.Context) error) error {
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

	ret := NewKeyLock(key)
	ret.Lock()
	defer ret.Unlock()

	err := f(ctx)
	if err != nil {
		return err
	}
	return nil
}

// keyLock 是一个可重入锁。
type keyLock struct {
	lock  sync.Mutex
	count atomic.Int64
}

// Lock 加锁。
func (k *keyLock) Lock() {
	k.count.Add(1)
	k.lock.Lock()
}

// Unlock 解锁。
func (k *keyLock) Unlock() {
	k.lock.Unlock()
	k.count.Add(-1)
}

// TryLock 尝试加锁。
func (k *keyLock) TryLock() bool {
	if k.lock.TryLock() {
		return true
	}
	k.count.Add(-1)
	return false
}
