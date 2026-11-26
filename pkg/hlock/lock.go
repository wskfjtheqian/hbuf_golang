package hlock

import "context"

// Locker
type Locker interface {
	Lock(ctx context.Context) error
	Unlock(ctx context.Context) error
	TryLock(ctx context.Context) (bool, error)
}

// 实现空锁
type EmptyLocker struct {
}

func (e *EmptyLocker) Lock(ctx context.Context) error {
	return nil
}

func (e *EmptyLocker) Unlock(ctx context.Context) error {
	return nil
}

func (e *EmptyLocker) TryLock(ctx context.Context) (bool, error) {
	return true, nil
}
