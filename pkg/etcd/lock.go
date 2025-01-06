package etcd

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// Mutex 互斥锁
type Mutex struct {
	mutex *concurrency.Mutex
	ctx   context.Context
}

// Lock 加锁
func Lock(ctx context.Context, pfx string) (*Mutex, error) {
	etcd, ok := FromContext(ctx)
	if !ok {
		return nil, erro.NewError("etcd not found in context")
	}
	client, err := etcd.GetClient()
	if err != nil {
		return nil, err
	}
	session, err := concurrency.NewSession(client)
	if err != nil {
		return nil, erro.Wrap(err)
	}
	l := &Mutex{
		ctx:   ctx,
		mutex: concurrency.NewMutex(session, "mutex-"+pfx),
	}
	err = l.mutex.Lock(ctx)
	if err != nil {
		return nil, erro.Wrap(err)
	}
	return l, nil
}

// TryLock 尝试加锁
func TryLock(ctx context.Context, pfx string) (*Mutex, error) {
	etcd, ok := FromContext(ctx)
	if !ok {
		return nil, erro.NewError("etcd not found in context")
	}
	client, err := etcd.GetClient()
	if err != nil {
		return nil, err
	}
	session, err := concurrency.NewSession(client)
	if err != nil {
		return nil, erro.Wrap(err)
	}

	l := &Mutex{
		ctx:   ctx,
		mutex: concurrency.NewMutex(session, "mutex-"+pfx),
	}
	err = l.mutex.TryLock(ctx)
	if err != nil {
		return nil, erro.Wrap(err)
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
		return erro.Wrap(err)
	}
	return nil
}
