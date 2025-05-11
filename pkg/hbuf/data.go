package hbuf

import (
	"io"
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

type Encoder struct {
	writer io.Writer
}

func NewEncoder(writer io.Writer) *Encoder {
	return &Encoder{writer: writer}
}

func (e *Encoder) Encode(data Data) error {
	return data.Descriptors().Encode(e.writer, data, 0)
}

type Decoder struct {
	reader io.Reader
}

func NewDecoder(reader io.Reader) *Decoder {
	return &Decoder{reader: reader}
}

func (d *Decoder) Decode(data Data) error {
	typ, _, valueLen, err := Reader(d.reader)
	if err != nil {
		return err
	}
	singlePtrType := reflect.TypeOf(data)
	doublePtr := reflect.New(singlePtrType)

	err = data.Descriptors().Decode(d.reader, doublePtr.Interface(), typ, valueLen)
	if err != nil {
		return err
	}
	// 通过反射将解码后的结果赋值给data
	reflect.ValueOf(data).Elem().Set(doublePtr.Elem().Elem())
	return nil
}
