package hctx

import "context"

type CloneableContext interface {
	Clone(ctx context.Context) context.Context
}

func CloneContext(oldCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	if val, ok := oldCtx.(CloneableContext); ok {
		ctx = val.Clone(ctx)
	}
	return ctx, cancelFunc
}
