package hlock

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hetcd"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// Mutex 互斥锁
type Mutex struct {
	mutex *concurrency.Mutex
	ctx   context.Context
}

// DcsLock 分布式控制系统加锁
func DcsLock(ctx context.Context, pfx string) (*Mutex, error) {
	e, ok := hetcd.FromContext(ctx)
	if !ok {
		return nil, herror.NewError("etcd not found in context")
	}
	client, err := e.GetClient()
	if err != nil {
		return nil, err
	}
	session, err := concurrency.NewSession(client)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	l := &Mutex{
		ctx:   ctx,
		mutex: concurrency.NewMutex(session, "mutex-"+pfx),
	}
	err = l.mutex.Lock(ctx)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	return l, nil
}

// TryDcsLock 分布式控制系统尝试加锁
func TryDcsLock(ctx context.Context, pfx string) (*Mutex, error) {
	e, ok := hetcd.FromContext(ctx)
	if !ok {
		return nil, herror.NewError("etcd not found in context")
	}
	client, err := e.GetClient()
	if err != nil {
		return nil, err
	}
	session, err := concurrency.NewSession(client)
	if err != nil {
		return nil, herror.Wrap(err)
	}

	l := &Mutex{
		ctx:   ctx,
		mutex: concurrency.NewMutex(session, "mutex-"+pfx),
	}
	err = l.mutex.TryLock(ctx)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	return l, nil
}

// Unlock 解锁
func (l *Mutex) Unlock() error {
	if nil == l.mutex {
		return nil
	}
	err := l.mutex.Unlock(l.ctx)
	if err != nil {
		return herror.Wrap(err)
	}
	return nil
}

// WithDcsLockFallback 带有 fallback 函数的本地锁。
func WithDcsLockFallback(ctx context.Context, key string, primary func(ctx context.Context) (bool, error), fallback func(ctx context.Context) error) error {
	ret, err := primary(ctx)
	if err != nil {
		return err
	}
	if ret {
		return nil
	}

	l, err := DcsLock(ctx, key)
	if err != nil {
		return err
	}
	defer l.Unlock()

	err = fallback(ctx)
	if err != nil {
		return err
	}
	return nil
}
