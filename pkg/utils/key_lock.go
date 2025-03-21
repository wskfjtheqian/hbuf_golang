package utl

import (
	"sync"
	"sync/atomic"
	"time"
)

var lockMap = make(map[string]*keyLock)
var lock sync.Mutex

func init() {
	ticker := time.NewTicker(time.Minute)
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

func NewKeyLock(key string) sync.Locker {
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

func KeyLock(key string) sync.Locker {
	ret := NewKeyLock(key)
	ret.Lock()
	return ret
}

type keyLock struct {
	lock  sync.Mutex
	count atomic.Int64
}

func (k *keyLock) Lock() {
	k.count.Add(1)
	k.lock.Lock()
}

func (k *keyLock) Unlock() {
	k.lock.Unlock()
	k.count.Add(-1)
}

func (k *keyLock) TryLock() bool {
	if k.lock.TryLock() {
		k.count.Add(-1)
		return true
	}
	return false
}
