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

func Marshal(data Data, tag string) ([]byte, error) {
	buf := make([]byte, 0, 128)
	return data.Descriptors().Encode(buf, reflect.ValueOf(data).UnsafePointer(), 0, false, tag), nil
}

func Unmarshal(buf []byte, data Data, tag string) (err error) {
	if len(buf) == 0 {
		return nil
	}
	//defer func() {
	//	if r := recover(); r!= nil {
	//		err = r.(error)
	//	}
	//}()
	typ, _, valueLen, buf := Reader(buf)

	doublePtr := reflect.New(reflect.TypeOf(data))
	_, err = data.Descriptors().Decode(buf, doublePtr.UnsafePointer(), typ, valueLen, tag)
	if err != nil {
		return err
	}

	if doublePtr.Elem().UnsafePointer() == nil {
		return nil
	}
	reflect.ValueOf(data).Elem().Set(doublePtr.Elem().Elem())
	return nil
}
