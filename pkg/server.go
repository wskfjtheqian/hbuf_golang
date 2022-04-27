package hbuf_golang

import (
	"context"
)

type ServerImp interface {
	Name() string

	Id() uint32
}

type ServerInvoke struct {
	Read   func(data []byte) (Data, error)
	Writer func(data Data) ([]byte, error)
	Call   func(context context.Context, data Data) (Data, error)
}

type InvokeData = func(context context.Context, data []byte) ([]byte, error)

type ServerRoute interface {
	Name() string

	Id() uint16

	InvokeMap() map[string]*ServerInvoke

	InvokeData() map[int64]InvokeData
}
