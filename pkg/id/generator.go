package id

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
)

// Generator Id生成器接口
type Generator interface {
	NextId(ctx context.Context) (hbuf.Int64, error)

	NextIds(ctx context.Context, count uint) ([]hbuf.Int64, error)
}
