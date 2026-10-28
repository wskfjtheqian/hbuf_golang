package hlock

import (
	"context"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hetcd"
	"go.etcd.io/etcd/client/v3/concurrency"
)

type etcdLock struct {
	mutex   *concurrency.Mutex
	session *concurrency.Session
}

// NewEtcdLock 创建 etcd 锁。每个锁实例持有独立的 session（lease），
// 锁 key 由 pfx + leaseID 构成，共享 session 会导致不同锁实例生成相同的锁 key 而互斥失效。
func NewEtcdLock(ctx context.Context, pfx string) (Locker, error) {
	client, ok := hetcd.FromContext(ctx)
	if !ok {
		return nil, herror.NewError("etcd not found in context")
	}

	conn, err := client.GetClient()
	if err != nil {
		return nil, err
	}

	session, err := concurrency.NewSession(conn)
	if err != nil {
		return nil, herror.Wrap(err)
	}

	return &etcdLock{
		mutex:   concurrency.NewMutex(session, "mutex-"+pfx),
		session: session,
	}, nil
}

// TryLock 尝试加锁。失败（锁被他人持有）不关闭 session，锁实例可继续重试。
func (l *etcdLock) TryLock(ctx context.Context) (bool, error) {
	err := l.mutex.TryLock(ctx)
	if err != nil {
		if err == concurrency.ErrLocked {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Lock 阻塞等待加锁。失败（如 ctx 取消）后锁实例不可复用，关闭 session 释放 lease。
func (l *etcdLock) Lock(ctx context.Context) error {
	err := l.mutex.Lock(ctx)
	if err != nil {
		if l.session != nil {
			_ = l.session.Close()
		}
		return herror.Wrap(err)
	}
	return nil
}

// Unlock 解锁并关闭 session。解锁失败时关闭 session 可让 lease 过期兜底释放锁。
func (l *etcdLock) Unlock(ctx context.Context) error {
	if nil == l.mutex {
		return nil
	}
	err := l.mutex.Unlock(ctx)
	if l.session != nil {
		_ = l.session.Close()
	}
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
	if !lock {
		// 本地锁失败，释放本地锁实例引用
		_ = l.local.Unlock(ctx)
		return false, nil
	}

	lock, err := l.etcd.TryLock(ctx)
	if !lock || err != nil {
		// etcd 加锁失败，释放已持有的本地锁
		_ = l.local.Unlock(ctx)
	}
	return lock, err
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
