package hid

import (
	"context"
)

// Generator Id生成器接口
type Generator interface {
	// NextId 获取下一个Id
	NextId(ctx context.Context) (int64, error)

	// NextIds 获取多个Id
	NextIds(ctx context.Context, count uint) ([]int64, error)
}
