package hlog

import (
	"context"
	"encoding/binary"
	"math/big"
	"math/rand/v2"
	"reflect"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

// 预定义 Base62 的字符集（共 62 个字符）
const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func NewTraceId() string {
	bytes := make([]byte, 16)
	// 1. 前 8 字节：毫秒时间戳（大端序）
	milli := uint64(htime.NowTime().UnixNano())
	binary.BigEndian.PutUint64(bytes[0:8], milli)
	binary.BigEndian.PutUint64(bytes[8:16], rand.Uint64())
	// 3. 将 16 字节的 byte 数组转换为 big.Int
	var num big.Int
	num.SetBytes(bytes)

	// 4. 执行 Base62 编码（128位整数转 Base62 刚好固定需要 22 位空间）
	result := make([]byte, 22)
	target := big.NewInt(62)
	rem := new(big.Int)

	for i := 21; i >= 0; i-- {
		num.DivMod(&num, target, rem)
		result[i] = base62Chars[rem.Int64()]
	}

	return string(result)
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
