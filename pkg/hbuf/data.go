package hbuf

import (
	"github.com/shopspring/decimal"
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

type Data interface {
	Descriptor() map[uint16]Descriptor
}

type Descriptor interface {
	Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error)
	Encode(writer io.Writer, d any, id uint16) (err error)
}

type Int8Descriptor struct {
	get func(d any) *int8
	set func(d any, v int8)
}

func NewInt8Descriptor(get func(d any) *int8, set func(d any, v int8)) Descriptor {
	return &Int8Descriptor{get: get, set: set}
}

func (i *Int8Descriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeInt64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	i.set(d, int8(value))
	return
}

func (i *Int8Descriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := i.get(d)
	if val == nil || *val == 0 {
		return nil
	}
	return EncodeInt64(writer, id, int64(*val))
}

type Int16Descriptor struct {
	get func(d any) *int16
	set func(d any, v int16)
}

func NewInt16Descriptor(get func(d any) *int16, set func(d any, v int16)) Descriptor {
	return &Int16Descriptor{get: get, set: set}
}

func (i *Int16Descriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeInt64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	i.set(d, int16(value))
	return
}

func (i *Int16Descriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := i.get(d)
	if val == nil || *val == 0 {
		return nil
	}
	return EncodeInt64(writer, id, int64(*val))
}

type Int32Descriptor struct {
	get func(d any) *int32
	set func(d any, v int32)
}

func NewInt32Descriptor(get func(d any) *int32, set func(d any, v int32)) Descriptor {
	return &Int32Descriptor{get: get, set: set}
}

func (i *Int32Descriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeInt64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	i.set(d, int32(value))
	return
}

func (i *Int32Descriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := i.get(d)
	if val == nil || *val == 0 {
		return nil
	}
	return EncodeInt64(writer, id, int64(*val))
}

type Int64Descriptor struct {
	get func(d any) *Int64
	set func(d any, v Int64)
}

func NewInt64Descriptor(get func(d any) *Int64, set func(d any, v Int64)) Descriptor {
	return &Int64Descriptor{get: get, set: set}
}

func (i *Int64Descriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeInt64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	i.set(d, Int64(value))
	return
}

func (i *Int64Descriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := i.get(d)
	if val == nil || *val == 0 {
		return nil
	}
	return EncodeInt64(writer, id, int64(*val))
}

type Uint8Descriptor struct {
	get func(d any) *uint8
	set func(d any, v uint8)
}

func NewUint8Descriptor(get func(d any) *uint8, set func(d any, v uint8)) Descriptor {
	return &Uint8Descriptor{get: get, set: set}
}

func (u *Uint8Descriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	u.set(d, uint8(value))
	return
}

func (u *Uint8Descriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := u.get(d)
	if val == nil || *val == 0 {
		return nil
	}
	return EncodeUint64(writer, id, uint64(*val))
}

type Uint16Descriptor struct {
	get func(d any) *uint16
	set func(d any, v uint16)
}

func NewUint16Descriptor(get func(d any) *uint16, set func(d any, v uint16)) Descriptor {
	return &Uint16Descriptor{get: get, set: set}
}

func (u *Uint16Descriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	u.set(d, uint16(value))
	return
}

func (u *Uint16Descriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := u.get(d)
	if val == nil || *val == 0 {
		return nil
	}
	return EncodeUint64(writer, id, uint64(*val))
}

type Uint32Descriptor struct {
	get func(d any) *uint32
	set func(d any, v uint32)
}

func NewUint32Descriptor(get func(d any) *uint32, set func(d any, v uint32)) Descriptor {
	return &Uint32Descriptor{get: get, set: set}
}

func (u *Uint32Descriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	u.set(d, uint32(value))
	return
}

func (u *Uint32Descriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := u.get(d)
	if val == nil || *val == 0 {
		return nil
	}
	return EncodeUint64(writer, id, uint64(*val))
}

type Uint64Descriptor struct {
	get func(d any) *Uint64
	set func(d any, v Uint64)
}

func NewUint64Descriptor(get func(d any) *Uint64, set func(d any, v Uint64)) Descriptor {
	return &Uint64Descriptor{get: get, set: set}
}

func (u *Uint64Descriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	u.set(d, Uint64(value))
	return
}

func (u *Uint64Descriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := u.get(d)
	if val == nil || *val == 0 {
		return nil
	}
	return EncodeUint64(writer, id, uint64(*val))
}

type FloatDescriptor struct {
	get func(d any) *float32
	set func(d any, v float32)
}

func NewFloatDescriptor(get func(d any) *float32, set func(d any, v float32)) Descriptor {
	return &FloatDescriptor{get: get, set: set}
}

func (f *FloatDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeFloat(reader, typ, valueLen)
	if err != nil {
		return err
	}
	f.set(d, value)
	return
}

func (f *FloatDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := f.get(d)
	if val == nil || *val == 0 {
		return nil
	}
	return EncodeFloat(writer, id, *val)
}

type DoubleDescriptor struct {
	get func(d any) *float64
	set func(d any, v float64)
}

func NewDoubleDescriptor(get func(d any) *float64, set func(d any, v float64)) Descriptor {
	return &DoubleDescriptor{get: get, set: set}
}

func (f *DoubleDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeDouble(reader, typ, valueLen)
	if err != nil {
		return err
	}
	f.set(d, value)
	return
}

func (f *DoubleDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := f.get(d)
	if val == nil || *val == 0 {
		return nil
	}
	return EncodeDouble(writer, id, *val)
}

type BytesDescriptor struct {
	get func(d any) []byte
	set func(d any, v []byte)
}

func NewBytesDescriptor(get func(d any) []byte, set func(d any, v []byte)) Descriptor {
	return &BytesDescriptor{get: get, set: set}
}

func (b *BytesDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeBytes(reader, typ, valueLen)
	if err != nil {
		return err
	}
	b.set(d, value)
	return
}

func (b *BytesDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := b.get(d)
	if val == nil || len(val) == 0 {
		return nil
	}
	return EncodeBytes(writer, id, b.get(d))
}

type BoolDescriptor struct {
	get func(d any) *bool
	set func(d any, v bool)
}

func NewBoolDescriptor(get func(d any) *bool, set func(d any, v bool)) Descriptor {
	return &BoolDescriptor{get: get, set: set}
}

func (b *BoolDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeBool(reader, typ, valueLen)
	if err != nil {
		return err
	}
	b.set(d, value)
	return
}

func (b *BoolDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	val := b.get(d)
	if val == nil || !*val {
		return nil
	}
	return EncodeBool(writer, id, *val)
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

func (l *ListDescriptor[T]) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
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

func (l *ListDescriptor[T]) Encode(writer io.Writer, d any, id uint16) (err error) {
	list := l.get(d).([]T)
	count := uint64(len(list))
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

func (m *MapDescriptor[K, V]) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
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

func (m *MapDescriptor[K, V]) Encode(writer io.Writer, d any, id uint16) (err error) {
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

type StructDescriptor[T Data] struct {
	get func(d any) T
	set func(d any, v T)
}

func NewDataDescriptor[T Data](get func(d any) T, set func(d any, v T)) Descriptor {
	return &StructDescriptor[T]{get: get, set: set}
}

func (s *StructDescriptor[T]) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	data := s.get(d)
	descMap := data.Descriptor()
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

func (s *StructDescriptor[T]) Encode(writer io.Writer, d any, id uint16) (err error) {
	data := s.get(d)
	if any(data) == nil {
		return
	}
	descMap := data.Descriptor()
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

type StringDescriptor struct {
	get func(d any) *string
	set func(d any, v string)
}

func NewStringDescriptor(get func(d any) *string, set func(d any, v string)) Descriptor {
	return &StringDescriptor{get: get, set: set}
}

func (s *StringDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeBytes(reader, typ, valueLen)
	if err != nil {
		return err
	}
	s.set(d, string(value))
	return
}

func (s *StringDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	value := s.get(d)
	if value == nil || len(*value) == 0 {
		return nil
	}
	return EncodeBytes(writer, id, []byte(*value))
}

type TimeDescriptor struct {
	get func(d any) *Time
	set func(d any, v Time)
}

func NewTimeDescriptor(get func(d any) *Time, set func(d any, v Time)) Descriptor {
	return &TimeDescriptor{get: get, set: set}
}

func (t *TimeDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	t.set(d, Time(time.UnixMilli(int64(value))))
	return
}

func (t *TimeDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	value := t.get(d)
	if value == nil || time.Time(*value).IsZero() {
		return nil
	}
	return EncodeUint64(writer, id, uint64(time.Time(*value).UnixMilli()))
}

type DecimalDescriptor struct {
	get func(d any) *decimal.Decimal
	set func(d any, v decimal.Decimal)
}

func NewDecimalDescriptor(get func(d any) *decimal.Decimal, set func(d any, v decimal.Decimal)) Descriptor {
	return &DecimalDescriptor{get: get, set: set}
}

func (s *DecimalDescriptor) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeBytes(reader, typ, valueLen)
	if err != nil {
		return err
	}
	num, err := decimal.NewFromString(string(value))
	if err != nil {
		return err
	}
	s.set(d, num)
	return
}

func (s *DecimalDescriptor) Encode(writer io.Writer, d any, id uint16) (err error) {
	value := s.get(d)
	if value == nil || value.IsZero() {
		return nil
	}

	return EncodeBytes(writer, id, []byte((*value).String()))
}

type IntE interface {
	int
}

type EnumDescriptor[T any] struct {
	get func(d any) *T
	set func(d any, v T)
}

func NewEnumDescriptor[T any](get func(d any) *T, set func(d any, v T)) Descriptor {
	return &EnumDescriptor[T]{get: get, set: set}
}

func (e *EnumDescriptor[T]) Decode(reader io.Reader, d any, typ Type, valueLen uint8) (err error) {
	value, err := DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return err
	}
	e.set(d, any(value).(T))
	return
}

func (e *EnumDescriptor[T]) Encode(writer io.Writer, d any, id uint16) (err error) {
	value := e.get(d)
	if value == nil {
		return nil
	}
	return EncodeUint64(writer, id, (any(*value)).(uint64))
}
