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

// EtcdLock 分布式控制系统加锁
func EtcdLock(ctx context.Context, pfx string) (*Mutex, error) {
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

// TryEtcdLock 分布式控制系统尝试加锁
func TryEtcdLock(ctx context.Context, pfx string) (*Mutex, error) {
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

// WithEtcdLockGetOrPopulate 带有 fallback 函数的分布式控制系统加锁。
func WithEtcdLockGetOrPopulate[T any](ctx context.Context, key string, get func(ctx context.Context) (T, bool, error), populate func(ctx context.Context) (T, error)) (T, error) {
	val, ret, err := get(ctx)
	if err != nil {
		return val, err
	}
	if ret {
		return val, nil
	}

	l, err := EtcdLock(ctx, key)
	if err != nil {
		return val, err
	}
	defer l.Unlock()

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

// WithEtcdLock 分布式控制系统加锁。
func WithEtcdLock[T any](ctx context.Context, key string, fun func(ctx context.Context) (T, error)) (T, error) {
	var val T
	l, err := EtcdLock(ctx, key)
	if err != nil {
		return val, err
	}
	defer l.Unlock()

	val, err = fun(ctx)
	if err != nil {
		return val, err
	}
	return val, nil
}

// WithTryEtcdLock 分布式控制系统尝试加锁。
func WithTryEtcdLock[T any](ctx context.Context, key string, fun func(ctx context.Context) (T, error)) (T, error) {
	var val T
	l, err := TryEtcdLock(ctx, key)
	if err != nil {
		return val, err
	}
	defer l.Unlock()

	val, err = fun(ctx)
	if err != nil {
		return val, err
	}
	return val, nil
}
