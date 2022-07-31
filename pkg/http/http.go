package http

import (
	"reflect"
)

func IsNil(i any) bool {
	defer func() {
		recover()
	}()
	return reflect.ValueOf(i).IsNil()
}
