package erro

import (
	"errors"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"reflect"
	"runtime/debug"
)

type Error struct {
	error
	stack []byte
}

func NewError(msg string) *Error {
	return &Error{
		error: errors.New(msg),
		stack: debug.Stack(),
	}
}

func Wrap(err error) *Error {
	return &Error{
		error: err,
		stack: debug.Stack(),
	}
}

func (e *Error) PrintStack() {
	_ = hlog.Output(2, hlog.ERROR, e.Error())
	_ = hlog.Output(2, hlog.ERROR, string(e.stack))
}

func (e *Error) Unwrap() error { return e.error }

var errorType = reflect.TypeOf(&Error{})

func IsError(err error) bool {
	return reflect.TypeOf(err) == errorType
}

func PrintStack(e error) {
	var err *Error
	if errors.As(e, &err) {
		err.PrintStack()
	} else {
		_ = hlog.Output(2, hlog.ERROR, e.Error())
	}
}
