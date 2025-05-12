package hbuf

import (
	"reflect"
)

type Type byte

const (
	TInt = iota + 0
	TUint
	TFloat
	TBool
	TBytes
	TList
	TMap
	TData
)

type Data interface {
	Descriptors() Descriptor
}

func Marshal(data Data) ([]byte, error) {
	buf := make([]byte, 0)
	return data.Descriptors().Encode(buf, reflect.ValueOf(data).UnsafePointer(), 0), nil
}

func Unmarshal(buf []byte, data Data) error {
	if len(buf) == 0 {
		return nil
	}
	typ, _, valueLen, buf := Reader(buf)

	doublePtr := reflect.New(reflect.TypeOf(data))
	_, err := data.Descriptors().Decode(buf, doublePtr.UnsafePointer(), typ, valueLen)
	if err != nil {
		return err
	}

	reflect.ValueOf(data).Elem().Set(doublePtr.Elem().Elem())
	return nil
}
