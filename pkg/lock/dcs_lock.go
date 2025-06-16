package lock

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// Mutex 互斥锁
type Mutex struct {
	mutex *concurrency.Mutex
	ctx   context.Context
}

// DcsLock 分布式控制系统加锁
func DcsLock(ctx context.Context, pfx string) (*Mutex, error) {
	e, ok := etcd.FromContext(ctx)
	if !ok {
		return nil, erro.NewError("etcd not found in context")
	}
	client, err := e.GetClient()
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

// TryDcsLock 分布式控制系统尝试加锁
func TryDcsLock(ctx context.Context, pfx string) (*Mutex, error) {
	e, ok := etcd.FromContext(ctx)
	if !ok {
		return nil, erro.NewError("etcd not found in context")
	}
	client, err := e.GetClient()
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
