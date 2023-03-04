package utl

import (
	"errors"
	"os"
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
	os.Stderr.WriteString(e.Error())
	_, _ = os.Stderr.Write(e.stack)
}

func (e *Error) Unwrap() error { return e.error }

var errorType = reflect.TypeOf(&Error{})

func IsError(err error) bool {
	return reflect.TypeOf(err) == errorType
}

func PrintStack(e error) {
	if err, ok := e.(*Error); ok {
		err.PrintStack()
	} else {
		os.Stderr.WriteString(e.Error())
	}
}
