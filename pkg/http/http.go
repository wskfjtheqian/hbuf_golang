package http

import (
	"reflect"
)

func IsNil(i interface{}) bool {
	defer func() {
		recover()
	}()
	return reflect.ValueOf(i).IsNil()
}
