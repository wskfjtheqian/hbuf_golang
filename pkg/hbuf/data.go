package hbuf

import (
	"google.golang.org/genproto/googleapis/type/decimal"
	"io"
	"time"
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

type Int64 int64

type Uint64 uint64

type Time time.Time

type Decimal decimal.Decimal

type Data interface {
	Descriptor() map[uint16]Descriptor
}

type Descriptor interface {
	Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error)
	Encode(writer io.Writer, d any, id uint16) (err error)
}

type Int64Descriptor struct {
	get func(d any) int64
	set func(d any, v int64)
}

func NewInt64Descriptor(get func(d any) int64, set func(d any, v int64)) Descriptor {
	return &Int64Descriptor{get: get, set: set}
}

func (i Int64Descriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeInt64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	i.set(d, value)
	return
}

func (i Int64Descriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	return EncodeInt64(writer, id, i.get(d))
}

type Uint64Descriptor struct {
	get func(d any) uint64
	set func(d any, v uint64)
}

func NewUint64Descriptor(get func(d any) uint64, set func(d any, v uint64)) Descriptor {
	return &Uint64Descriptor{get: get, set: set}
}

func (u Uint64Descriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	u.set(d, value)
	return
}

func (u Uint64Descriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	return EncodeUint64(writer, id, u.get(d))
}

type FloatDescriptor struct {
	get func(d any) float32
	set func(d any, v float32)
}

func NewFloatDescriptor(get func(d any) float32, set func(d any, v float32)) Descriptor {
	return &FloatDescriptor{get: get, set: set}
}

func (f FloatDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeFloat(reader, typ, valueLen)
	if err != nil {
		return err
	}
	f.set(d, value)
	return
}

func (f FloatDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	return EncodeFloat(writer, id, f.get(d))
}

type DoubleDescriptor struct {
	get func(d any) float64
	set func(d any, v float64)
}

func NewDoubleDescriptor(get func(d any) float64, set func(d any, v float64)) Descriptor {
	return &DoubleDescriptor{get: get, set: set}
}

func (f DoubleDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeDouble(reader, typ, valueLen)
	if err != nil {
		return err
	}
	f.set(d, value)
	return
}

func (f DoubleDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	return EncodeDouble(writer, id, f.get(d))
}

type BytesDescriptor struct {
	get func(d any) []byte
	set func(d any, v []byte)
}

func NewBytesDescriptor(get func(d any) []byte, set func(d any, v []byte)) Descriptor {
	return &BytesDescriptor{get: get, set: set}
}

func (b BytesDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeBytes(reader, typ, valueLen)
	if err != nil {
		return err
	}
	b.set(d, value)
	return
}

func (b BytesDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	return EncodeBytes(writer, id, b.get(d))
}

type BoolDescriptor struct {
	get func(d any) bool
	set func(d any, v bool)
}

func NewBoolDescriptor(get func(d any) bool, set func(d any, v bool)) Descriptor {
	return &BoolDescriptor{get: get, set: set}
}

func (b BoolDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeBool(reader, typ, valueLen)
	if err != nil {
		return err
	}
	b.set(d, value)
	return
}

func (b BoolDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	return EncodeBool(writer, id, b.get(d))
}

type ListDescriptor[T any] struct {
	get  func(d any) any
	set  func(d any, v any)
	desc Descriptor
}

func NewListDescriptor[T any](
	get func(d any) any,
	set func(d any, v any),
	desc Descriptor,
) Descriptor {
	return &ListDescriptor[T]{
		get:  get,
		set:  set,
		desc: desc,
	}
}

func (l ListDescriptor[T]) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	var count uint64
	count, err = DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return
	}

	list := make([]T, count)
	for i := uint64(0); i < count; i++ {
		typ, _, valueLen, err = Reader(reader)
		if err != nil {
			return
		}
		err = l.desc.Decode(reader, &list[i], typ, valueLen)
		if err != nil {
			return
		}
	}
	l.set(d, list)
	return nil
}

func (l ListDescriptor[T]) Encode(writer io.Writer, d any, id uint16) (err error) {
	list := l.get(d).([]T)
	count := uint64(len(list))
	if count == 0 {
		return
	}

	err = Writer(writer, TList, id, LengthUint(count))
	if err != nil {
		return
	}

	err = WriterUint64(writer, count)
	if err != nil {
		return
	}
	for _, v := range list {
		err = l.desc.Encode(writer, v, 0)
		if err != nil {
			return
		}
	}
	return nil
}

type MapDescriptor[K comparable, V any] struct {
	get func(d any) any
	set func(d any, v any)
	key Descriptor
	val Descriptor
}

func NewMapDescriptor[K comparable, V any](
	get func(d any) any,
	set func(d any, v any),
	key Descriptor,
	val Descriptor,
) Descriptor {
	return &MapDescriptor[K, V]{
		get: get,
		set: set,
		key: key,
		val: val,
	}
}

func (m MapDescriptor[K, V]) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	var count uint64
	count, err = DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return
	}

	maps := make(map[K]V, count)
	for i := uint64(0); i < count; i++ {
		typ, _, valueLen, err = Reader(reader)
		if err != nil {
			return
		}
		var key K
		err = m.key.Decode(reader, &key, typ, valueLen)
		if err != nil {
			return
		}

		typ, _, valueLen, err = Reader(reader)
		if err != nil {
			return
		}
		var value V
		err = m.val.Decode(reader, &value, typ, valueLen)
		if err != nil {
			return
		}
		maps[key] = value
	}
	m.set(d, maps)
	return nil
}

func (m MapDescriptor[K, V]) Encode(writer io.Writer, d any, id uint16) (err error) {
	maps := m.get(d).(map[K]V)
	count := uint64(len(maps))
	if count == 0 {
		return
	}

	err = Writer(writer, TMap, id, LengthUint(count))
	if err != nil {
		return
	}

	err = WriterUint64(writer, count)
	if err != nil {
		return
	}
	for key, val := range maps {
		err = m.key.Encode(writer, key, 0)
		if err != nil {
			return
		}
		err = m.val.Encode(writer, val, 0)
		if err != nil {
			return
		}
	}
	return nil
}

type StructDescriptor struct {
	get func(d any) any
	set func(d any, v any)
}

func NewStructDescriptor(get func(d any) any, set func(d any, v any)) Descriptor {
	return &StructDescriptor{get: get, set: set}
}

func (s StructDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	data := s.get(d)
	descMap := data.(Data).Descriptor()
	if descMap == nil {
		return
	}

	count, err := DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return
	}

	var id uint16
	for i := uint64(0); i < count; i++ {
		typ, id, valueLen, err = Reader(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return
		}
		if desc, ok := descMap[id]; ok {
			err = desc.Decode(reader, data, typ, valueLen)
			if err != nil {
				return
			}
		}
	}
	s.set(d, data)
	return
}

func (s StructDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	data := s.get(d)
	descMap := data.(Data).Descriptor()
	if descMap == nil {
		return
	}

	count := uint64(len(descMap))
	err = Writer(writer, TData, id, LengthUint(count))
	if err != nil {
		return
	}

	err = WriterUint64(writer, count)
	if err != nil {
		return err
	}

	for i, field := range descMap {
		err = field.Encode(writer, data, i)
		if err != nil {
			return
		}
	}
	return
}
