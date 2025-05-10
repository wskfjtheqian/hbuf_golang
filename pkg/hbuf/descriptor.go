package hbuf

import (
	"errors"
	"io"
)

type Descriptor interface {
	IsEmpty(v any) bool
	Encode(writer io.Writer, v any, id uint16) (err error)
	Decode(reader io.Reader, v any, typ Type, valueLen uint8) (err error)
}

type DataDescriptor[T Data] struct {
	get      func(v any) T
	set      func(v any, value T)
	create   func() T
	fieldMap map[uint16]Descriptor
	fields   []Descriptor
	ids      []uint16
}

func NewDataDescriptor[T Data](get func(v any) T, set func(v any, value T), create func() T) *DataDescriptor[T] {
	return &DataDescriptor[T]{
		fieldMap: make(map[uint16]Descriptor),
		fields:   make([]Descriptor, 0),
		ids:      make([]uint16, 0),
		get:      get,
		set:      set,
		create:   create,
	}
}

func CloneDataDescriptor[T Data](get func(v any) T, set func(v any, value T), desc *DataDescriptor[T]) *DataDescriptor[T] {
	return &DataDescriptor[T]{
		fieldMap: desc.fieldMap,
		fields:   desc.fields,
		ids:      desc.ids,
		get:      get,
		set:      set,
		create:   desc.create,
	}
}

func (d *DataDescriptor[T]) IsEmpty(v any) bool {
	//return d.get(v) == nil
	return false
}

func (d *DataDescriptor[T]) AddField(id uint16, field Descriptor) *DataDescriptor[T] {
	d.fieldMap[id] = field
	d.fields = append(d.fields, field)
	d.ids = append(d.ids, id)
	return d
}

func (d *DataDescriptor[T]) Encode(writer io.Writer, v any, id uint16) error {
	val := d.get(v)
	count := 0
	for _, field := range d.fields {
		if !field.IsEmpty(val) {
			count++
		}
	}

	length := LengthUint(uint64(count))
	err := Writer(writer, TData, id, length)
	if err != nil {
		return err
	}

	err = WriterUint64(writer, uint64(count))
	if err != nil {
		return err
	}

	for fieldId, field := range d.fields {
		err = field.Encode(writer, val, d.ids[fieldId])
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *DataDescriptor[T]) Decode(reader io.Reader, v any, typ Type, valueLen uint8) (err error) {
	if typ != TData {
		err = errors.New("invalid data type")
	}

	var count uint64
	count, err = DecodeUint64(reader, TUint, valueLen)
	if err != nil {
		return
	}

	var value = d.create()

	var id uint16
	for i := uint64(0); i < count; i++ {
		typ, id, valueLen, err = Reader(reader)
		if err != nil {
			return err
		}
		if field, ok := d.fieldMap[id]; ok {
			err = field.Decode(reader, value, typ, valueLen)
			if err != nil {
				return err
			}
		}
	}
	d.set(v, value)
	return
}

type ListDescriptor[T any] struct {
	get  func(v any) []T
	set  func(v any, value []T)
	desc Descriptor
}

func NewListDescriptor[T any](get func(v any) []T, set func(v any, value []T), desc Descriptor) Descriptor {
	return &ListDescriptor[T]{desc: desc, get: get, set: set}
}

func (l *ListDescriptor[T]) IsEmpty(v any) bool {
	return len(l.get(v)) == 0
}

func (l *ListDescriptor[T]) Encode(writer io.Writer, v any, id uint16) error {
	val := l.get(v)

	count := len(val)
	err := Writer(writer, TList, id, LengthUint(uint64(count)))
	if err != nil {
		return err
	}
	err = WriterUint64(writer, uint64(count))
	if err != nil {
		return err
	}

	for _, item := range val {
		err = l.desc.Encode(writer, item, 0)
		if err != nil {
			return err
		}
	}
	return nil
}

func (l *ListDescriptor[T]) Decode(reader io.Reader, v any, typ Type, valueLen uint8) (err error) {
	var count uint64
	count, err = DecodeUint64(reader, TInt, valueLen)
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
			return err
		}
	}
	l.set(v, list)
	return
}

type MapDescriptor[K comparable, V any] struct {
	get     func(v any) map[K]V
	set     func(v any, value map[K]V)
	keyDesc Descriptor
	valDesc Descriptor
}

func NewMapDescriptor[K comparable, V any](get func(v any) map[K]V, set func(v any, value map[K]V), keyDesc Descriptor, valDesc Descriptor) Descriptor {
	return &MapDescriptor[K, V]{keyDesc: keyDesc, valDesc: valDesc, get: get, set: set}
}

func (m *MapDescriptor[K, V]) IsEmpty(v any) bool {
	return len(m.get(v)) == 0
}

func (m *MapDescriptor[K, V]) Encode(writer io.Writer, v any, id uint16) error {
	val := m.get(v)

	count := len(val)
	err := Writer(writer, TMap, id, LengthUint(uint64(count)))
	if err != nil {
		return err
	}
	err = WriterInt64(writer, int64(count))
	if err != nil {
		return err
	}

	for key, item := range val {
		err = m.keyDesc.Encode(writer, key, 0)
		if err != nil {
			return err
		}
		err = m.valDesc.Encode(writer, item, 0)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *MapDescriptor[K, V]) Decode(reader io.Reader, v any, typ Type, valueLen uint8) (err error) {
	var count uint64
	count, err = DecodeUint64(reader, TInt, valueLen)
	if err != nil {
		return
	}

	mapVal := make(map[K]V, count)
	for i := uint64(0); i < count; i++ {
		typ, _, valueLen, err = Reader(reader)
		if err != nil {
			return
		}
		var key K
		err = m.keyDesc.Decode(reader, &key, typ, valueLen)
		if err != nil {
			return
		}

		typ, _, valueLen, err = Reader(reader)
		if err != nil {
			return
		}
		var val V
		err = m.valDesc.Decode(reader, &val, typ, valueLen)
		if err != nil {
			return
		}
		mapVal[key] = val
	}
	m.set(v, mapVal)
	return nil
}

type Int64Descriptor struct {
	get func(v any) *int64
	set func(v any, value int64)
}

func NewInt64Descriptor(get func(v any) *int64, set func(v any, value int64)) Descriptor {
	return &Int64Descriptor{get: get, set: set}
}

func (i *Int64Descriptor) IsEmpty(v any) bool {
	val := i.get(v)
	return val == nil || *val == 0
}

func (i *Int64Descriptor) Encode(writer io.Writer, v any, id uint16) error {
	val := i.get(v)

	err := Writer(writer, TInt, id, LengthInt(*val))
	if err != nil {
		return err
	}
	return WriterInt64(writer, int64(*val))
}

func (i *Int64Descriptor) Decode(reader io.Reader, v any, typ Type, valueLen uint8) (err error) {
	var value int64
	value, err = DecodeInt64(reader, typ, valueLen)
	if err != nil {
		return
	}
	i.set(v, value)
	return nil
}

type StringDescriptor struct {
	get func(v any) *string
	set func(v any, value string)
}

func NewStringDescriptor(get func(v any) *string, set func(v any, value string)) Descriptor {
	return &StringDescriptor{get: get, set: set}
}

func (s *StringDescriptor) IsEmpty(v any) bool {
	val := s.get(v)
	return val == nil || len(*val) == 0
}

func (s *StringDescriptor) Encode(writer io.Writer, v any, id uint16) (err error) {
	val := s.get(v)

	size := uint64(len(*val))
	err = Writer(writer, TBytes, id, LengthUint(size))
	if err != nil {
		return
	}
	err = WriterUint64(writer, size)
	if err != nil {
		return
	}
	_, err = writer.Write([]byte(*val))
	return err
}

func (s *StringDescriptor) Decode(reader io.Reader, v any, typ Type, valueLen uint8) (err error) {
	var size uint64
	size, err = DecodeUint64(reader, TUint, valueLen)
	if err != nil {
		return
	}

	msg := make([]byte, size)
	_, err = reader.Read(msg)
	if err != nil {
		return
	}
	s.set(v, string(msg))
	return nil
}
