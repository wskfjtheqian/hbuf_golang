package app

import (
	"testing"
)

// Handler 是用于处理RPC请求
type Handler func(value int32) (int32, error)

// HandlerMiddleware 用于对 Handler 进行中间件处理。
type HandlerMiddleware func(next Handler) Handler

type DD struct {
	middleware HandlerMiddleware
}

func NewDD(middleware ...HandlerMiddleware) *DD {
	ret := &DD{}
	for _, m := range middleware {
		ret.middleware = func(next Handler) Handler {
			return m(next)
		}
	}
	return ret
}

func TestNewApp(t *testing.T) {
	dd := NewDD(func(next Handler) Handler {
		return func(message int32) (int32, error) {
			return next(message + 2)
		}
	}, func(next Handler) Handler {
		return func(message int32) (int32, error) {
			return next(message + 3)
		}
	}, func(next Handler) Handler {
		return func(message int32) (int32, error) {
			return next(message + 4)
		}
	}, func(next Handler) Handler {
		return func(message int32) (int32, error) {
			return next(message + 5)
		}
	})

	t.Log(dd.middleware(func(message int32) (int32, error) {
		return message + 1, nil
	})(2))
}
