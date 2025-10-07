package hlock

// Locker 是一个可重入锁。
type Locker interface {
	Lock()
	Unlock()
	TryLock() bool
}
