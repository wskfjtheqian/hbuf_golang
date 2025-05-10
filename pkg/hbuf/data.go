package hbuf

import (
	"io"
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
	return data.Descriptors().Decode(d.reader, data, typ, valueLen)
}
