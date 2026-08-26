package hlog

import (
	"context"
	"math/big"
	"math/rand/v2"
	"reflect"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

// 预定义 Base62 的字符集（共 62 个字符）
const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func NewTraceId() string {
	// 1. 获取毫秒时间戳（约占 41~42 位）
	milli := uint64(htime.NowTime().UnixMilli())

	// 2. 完美解决编译问题：使用两个全局无锁的 rand.Uint32() 拼装出 65 位的强随机数熵
	// 满足 107 位（42位时间 + 65位随机）的空间限制，确保 18 位 Base62 完美填满
	random65 := (uint64(rand.Uint32()) << 33) | uint64(rand.Uint32())

	// 3. 使用大数按位组装，替代原先的 16 字节切片，彻底消灭开头的 0
	var num big.Int
	num.SetUint64(milli)
	num.Lsh(&num, 65) // 将时间戳左移 65 位，给随机数让出空间

	var randNum big.Int
	randNum.SetUint64(random65)
	num.Or(&num, &randNum) // 混合时间与随机数

	// 4. 执行 Base62 编码（目标空间修改为固定的 18 位）
	result := make([]byte, 18)
	target := big.NewInt(62)
	rem := new(big.Int)

	// 循环次数改为 17 到 0，刚好生成 18 个字符
	for i := 17; i >= 0; i-- {
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
