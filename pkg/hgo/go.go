package hgo

import (
	"context"
	"runtime/debug"
	"sync"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

var wait sync.WaitGroup

func Go(ctx context.Context, fn func(ctx context.Context) error) {
	go func() {
		wait.Add(1)
		defer func() {
			err := recover()
			if err != nil {
				hlog.Error(ctx, "%s \n", err, string(debug.Stack()))
			}
			wait.Done()
		}()

		ctx = hlog.WithContext(ctx, hlog.FromContext(ctx))
		err := fn(ctx)
		if err != nil {
			herror.PrintStack(ctx, err)
		}
	}()
}

func Wait() {
	wait.Wait()
}
