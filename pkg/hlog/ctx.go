package hlog

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"math/rand/v2"
	"reflect"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

func NewTraceId() string {
	bytes := make([]byte, 16)
	// 1. 前 8 字节：毫秒时间戳（大端序）
	milli := uint64(htime.NowTime().UnixNano())
	binary.BigEndian.PutUint64(bytes[0:8], milli)
	binary.BigEndian.PutUint64(bytes[8:16], rand.Uint64())
	return hex.EncodeToString(bytes)
}

// WithContext 给上下文添加 HTTP 连接
func WithContext(ctx context.Context, traceId string) context.Context {
	if len(traceId) == 0 {
		traceId = NewTraceId()
	}
	return &Context{
		Context: ctx,
		traceId: traceId,
	}
}

func NewContext() context.Context {
	return &Context{
		Context: context.Background(),
		traceId: NewTraceId(),
	}
}

type Context struct {
	context.Context
	traceId string
}

var contextType = reflect.TypeOf(&Context{})

// Value 返回Context的value
func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

// FromContext 从上下文中获取 HTTP 连接
func FromContext(ctx context.Context) string {
	val := ctx.Value(contextType)
	if val == nil {
		return ""
	}
	return val.(*Context).traceId
}
