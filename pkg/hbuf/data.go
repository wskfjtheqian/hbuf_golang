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

func Marshal(data Data) (b []byte, err error) {
	buf := make([]byte, 0, 128)
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return data.Descriptors().Encode(buf, reflect.ValueOf(data).UnsafePointer(), nil), nil
}

func Unmarshal(buf []byte, data Data) (err error) {
	if len(buf) == 0 {
		return nil
	}
	//defer func() {
	//	if r := recover(); r!= nil {
	//		err = r.(error)
	//	}
	//}()
	typ, _, valueLen, buf := DecodeType(buf)
	_, err = data.Descriptors().Decode(buf, reflect.ValueOf(data).UnsafePointer(), typ, true, valueLen)
	if err != nil {
		return err
	}
	return nil
}
