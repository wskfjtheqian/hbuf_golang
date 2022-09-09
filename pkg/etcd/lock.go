package etc

import (
	"context"
	utl "github.com/wskfjtheqian/hbuf_golang/pkg/utils"
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
