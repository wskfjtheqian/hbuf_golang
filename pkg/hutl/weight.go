package hutl

import (
	"context"
	"math/rand"
)

// NewWeight 权随机选择的工具。这个工具允许根据给定的权重从一组元素中随机选择一个元素
func NewWeight[T any, E comparable, F any](
	getList func(ctx context.Context, columns ...F) ([]T, error),
	getWeight func(ctx context.Context, val *T) ([]E, int32),
) *Weight[T, E, F] {
	return &Weight[T, E, F]{
		getList:   getList,
		getWeight: getWeight,
		weights:   make(map[E]int64),
		groups:    make(map[E][]*T),
	}
}

type Weight[T any, E comparable, F any] struct {
	getList   func(ctx context.Context, columns ...F) ([]T, error)
	getWeight func(ctx context.Context, val *T) ([]E, int32)
	weights   map[E]int64
	groups    map[E][]*T
}

func (t *Weight[T, E, F]) Init(ctx context.Context) error {
	list, err := t.getList(ctx)
	if err != nil {
		return err
	}
	for _, val := range list {
		typeList, weight := t.getWeight(ctx, &val)
		for _, item := range typeList {
			t.weights[item] += int64(weight)
			t.groups[item] = append(t.groups[item], &val)
		}
	}
	return nil
}

func (t *Weight[T, E, F]) Get(ctx context.Context, typ E) (*T, bool) {
	if len(t.groups[typ]) == 0 {
		return nil, false
	}
	total := rand.Int63n(t.weights[typ])
	for _, val := range t.groups[typ] {
		_, weight := t.getWeight(ctx, val)
		total -= int64(weight)
		if total < int64(weight) {
			return val, true
		}
	}
	return nil, false
}
