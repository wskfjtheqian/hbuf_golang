package herror

import (
	"errors"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"reflect"
	"runtime/debug"
)

// Error 是带有堆栈跟踪的错误包装器。
type Error struct {
	error
	stack []byte
}

// NewError 创建一个带有堆栈跟踪的错误。
func NewError(msg string) *Error {
	return &Error{
		error: errors.New(msg),
		stack: debug.Stack(),
	}
}

// Wrap 包装一个错误，并返回一个带有堆栈跟踪的错误。
func Wrap(err error) *Error {
	return &Error{
		error: err,
		stack: debug.Stack(),
	}
}

// PrintStack 打印错误的堆栈跟踪信息。
func (e *Error) PrintStack() {
	_ = hlog.Output(2, hlog.ERROR, e.Error())
	_ = hlog.Output(2, hlog.ERROR, string(e.stack))
}

// Unwrap 返回错误的底层错误。
func (e *Error) Unwrap() error { return e.error }

var errorType = reflect.TypeOf(&Error{})

// IsError 判断一个错误是否是 *Error。
func IsError(err error) bool {
	return reflect.TypeOf(err) == errorType
}

// PrintStack 打印一个错误的堆栈跟踪信息。
func PrintStack(e error) {
	var err *Error
	if errors.As(e, &err) {
		err.PrintStack()
	} else {
		_ = hlog.Output(2, hlog.ERROR, e.Error())
	}
}
