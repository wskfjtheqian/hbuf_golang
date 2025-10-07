package hid

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
)

// Generator Id生成器接口
type Generator interface {
	// NextId 获取下一个Id
	NextId(ctx context.Context) (hbuf.Int64, error)

	// NextIds 获取多个Id
	NextIds(ctx context.Context, count uint) ([]hbuf.Int64, error)
}
