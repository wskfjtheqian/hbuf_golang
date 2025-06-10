package etc

import (
	"context"
	utl "github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"go.etcd.io/etcd/client/v3/concurrency"
)

type EtcdLock struct {
	mutex *concurrency.Mutex
	ctx   context.Context
}

func Lock(ctx context.Context, pfx string) (*EtcdLock, error) {
	session, err := GET(ctx).GetSession(ctx)
	if err != nil {
		return nil, utl.Wrap(err)
	}
	l := &EtcdLock{
		ctx:   ctx,
		mutex: concurrency.NewMutex(session, "mutex-"+pfx),
	}
	err = l.mutex.Lock(ctx)
	if err != nil {
		return nil, utl.Wrap(err)
	}
	go func() {
		select {
		case <-ctx.Done():
			l.mutex = nil
		}
	}()
	return l, nil
}

func TryLock(ctx context.Context, pfx string) (*EtcdLock, error) {
	session, err := GET(ctx).GetSession(ctx)
	if err != nil {
		return nil, utl.Wrap(err)
	}
	l := &EtcdLock{
		ctx:   ctx,
		mutex: concurrency.NewMutex(session, "mutex-"+pfx),
	}
	err = l.mutex.TryLock(ctx)
	if err != nil {
		return nil, utl.Wrap(err)
	}
	go func() {
		select {
		case <-ctx.Done():
			l.mutex = nil
		}
	}()
	return l, nil
}

func (l *EtcdLock) Unlock() error {
	if nil == l.mutex {
		return nil
	}
	err := l.mutex.Unlock(l.ctx)
	if err != nil {
		return utl.Wrap(err)
	}
	return nil
}

// DoubleLock 双重锁
func DoubleLock(ctx context.Context, pfx string, fn func(ctx context.Context) (bool, error), fn2 func(ctx context.Context) error) error {
	b, err := fn(ctx)
	if err != nil {
		return err
	}
	if b {
		return nil
	}
	lock, err := Lock(ctx, pfx)
	if err != nil {
		return err
	}
	defer lock.Unlock()

	b, err = fn(ctx)
	if err != nil {
		return err
	}
	if b {
		return nil
	}

	return fn2(ctx)
}
