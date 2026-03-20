package hlock

import (
	"context"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hetcd"
	"go.etcd.io/etcd/client/v3/concurrency"
)

type etcdLock struct {
	mutex *concurrency.Mutex
}

func NewEtcdLock(ctx context.Context, pfx string) (Locker, error) {
	client, ok := hetcd.FromContext(ctx)
	if !ok {
		return nil, herror.NewError("etcd not found in context")
	}

	session, err := client.GetSession()
	if err != nil {
		return nil, herror.Wrap(err)
	}

	return &etcdLock{
		mutex: concurrency.NewMutex(session, "mutex-"+pfx),
	}, nil
}

// TryLock
func (l *etcdLock) TryLock(ctx context.Context) (bool, error) {
	err := l.mutex.TryLock(ctx)
	if err != nil {
		return false, err
	}
	return true, nil
}

// Lock
func (l *etcdLock) Lock(ctx context.Context) error {
	err := l.mutex.Lock(ctx)
	if err != nil {
		return herror.Wrap(err)
	}
	return nil
}

// Unlock
func (l *etcdLock) Unlock(ctx context.Context) error {
	if nil == l.mutex {
		return nil
	}
	err := l.mutex.Unlock(ctx)
	if err != nil {
		return herror.Wrap(err)
	}
	return nil
}

type localEtcd struct {
	local Locker
	etcd  Locker
}

func (l localEtcd) Lock(ctx context.Context) error {
	_ = l.local.Lock(ctx)
	return l.etcd.Lock(ctx)
}

func (l localEtcd) Unlock(ctx context.Context) error {
	_ = l.etcd.Unlock(ctx)
	_ = l.local.Unlock(ctx)
	return nil
}

func (l localEtcd) TryLock(ctx context.Context) (bool, error) {
	lock, _ := l.local.TryLock(ctx)
	if lock {
		return l.etcd.TryLock(ctx)
	}
	return false, nil
}

// WithEtcdGetOrPopulate 带有 fallback 函数的分布式控制系统加锁, 基于ctx的可重入锁,优先走本地锁。
func WithEtcdGetOrPopulate[T any](ctx context.Context, key string, get func(ctx context.Context) (T, bool, error), populate func(ctx context.Context) (T, error)) (T, error) {
	val, ret, err := get(ctx)
	if err != nil {
		return val, err
	}
	if ret {
		return val, nil
	}

	ctx, l, err := etcdContext(ctx, key)
	if err != nil {
		return val, err
	}
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

// WithEtcd 分布式控制系统加锁, 基于ctx的可重入锁,优先走本地锁。
func WithEtcd[T any](ctx context.Context, key string, fun func(ctx context.Context) (T, error)) (T, error) {
	var val T
	ctx, l, err := etcdContext(ctx, key)
	if err != nil {
		return val, err
	}
	err = l.Lock(ctx)
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

// WithEtcdTry 分布式控制系统尝试加锁, 基于ctx的可重入锁,优先走本地锁。
func WithEtcdTry[T any](ctx context.Context, key string, fun func(ctx context.Context) (T, error)) (T, error) {
	var val T
	ctx, l, err := etcdContext(ctx, key)
	if err != nil {
		return val, err
	}
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

func etcdContext(ctx context.Context, key string) (context.Context, Locker, error) {
	key = "etcd_lock_" + key
	value := ctx.Value(key)
	if value != nil {
		return ctx, value.(*etcdLock), nil
	}

	lock, err := NewEtcdLock(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	l := &localRedis{
		local: NewLocalLock(key),
		redis: lock,
	}

	ctx = context.WithValue(ctx, key, l)
	return ctx, l, nil
}
