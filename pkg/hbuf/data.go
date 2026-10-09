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
	return data.Descriptors().Encode(buf, reflect.ValueOf(data).UnsafePointer(), nil), nil
}

func Unmarshal(buf []byte, data Data) error {
	if len(buf) == 0 {
		return nil
	}
	typ, _, valueLen, buf, err := DecodeType(buf)
	if err != nil {
		return err
	}
	_, err = data.Descriptors().Decode(buf, reflect.ValueOf(data).UnsafePointer(), typ, true, valueLen)
	return err
}
