package hbuf

import (
	"errors"
	"reflect"
	"sort"
	"unsafe"
)

type Descriptor interface {
	IsEmpty(p unsafe.Pointer) bool
	Encode(buf []byte, p unsafe.Pointer, id uint16) []byte
	Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8) ([]byte, error)
}

func NewDataDescriptor(offset uintptr, isPtr bool, typ reflect.Type, fieldMap map[uint16]Descriptor) Descriptor {
	var fields []Descriptor
	var ids []uint16
	for id, _ := range fieldMap {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		fields = append(fields, fieldMap[id])
	}
	return &DataDescriptor{
		offset:   offset,
		fieldMap: fieldMap,
		fields:   fields,
		ids:      ids,
		isPtr:    isPtr,
		typ:      typ,
	}
}

func CloneDataDescriptor(d Data, offset uintptr, isPtr bool) Descriptor {
	var desc = d.Descriptors().(*DataDescriptor)
	return &DataDescriptor{
		offset:   offset,
		fieldMap: desc.fieldMap,
		fields:   desc.fields,
		ids:      desc.ids,
		isPtr:    isPtr,
		typ:      desc.typ,
	}
}

type DataDescriptor struct {
	offset   uintptr
	fieldMap map[uint16]Descriptor
	fields   []Descriptor
	ids      []uint16
	isPtr    bool
	typ      reflect.Type
}

func (d *DataDescriptor) IsEmpty(p unsafe.Pointer) bool {
	if d.isPtr {
		var data = unsafe.Add(p, d.offset)
		ref := *(*uintptr)(data)
		if ref == 0 {
			return true
		}
	}
	return false
}

func (d *DataDescriptor) Encode(buf []byte, p unsafe.Pointer, id uint16) []byte {
	var data = unsafe.Add(p, d.offset)
	if d.isPtr {
		ref := *(*uintptr)(data)
		if ref == 0 {
			return buf
		}
		data = unsafe.Pointer(ref)
	}

	count := uint64(0)
	for _, desc := range d.fields {
		if !desc.IsEmpty(data) {
			count++
		}
	}
	if count == 0 {
		return buf
	}
	buf = WriterTypeId(buf, TData, id, LengthUint(count))
	buf = WriterUint64(buf, count)

	for i, desc := range d.fields {
		buf = desc.Encode(buf, data, d.ids[i])
	}
	return buf
}

func (d *DataDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8) ([]byte, error) {
	if typ != TData {
		return nil, errors.New("invalid data type")
	}
	var err error
	var id uint16

	data := reflect.New(d.typ)
	ptr := data.UnsafePointer()

	count, buf := DecodeUint64(buf, valueLen)
	for i := uint64(0); i < count; i++ {
		typ, id, valueLen, buf = Reader(buf)

		if field, ok := d.fieldMap[id]; ok {
			buf, err = field.Decode(buf, ptr, typ, valueLen)
			if err != nil {
				return nil, err
			}
		}
	}
	*(*unsafe.Pointer)(unsafe.Add(p, d.offset)) = ptr
	return buf, nil
}

func (d *DataDescriptor) Fields() map[uint16]Descriptor {
	return d.fieldMap
}

func NewInt64Descriptor(offset uintptr, isPrt bool) Descriptor {
	return &Int64Descriptor{offset: offset, isPrt: isPrt}
}

type Int64Descriptor struct {
	offset uintptr
	isPrt  bool
}

func (d *Int64Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8) ([]byte, error) {
	val, buf := DecodeInt64(buf, valueLen)
	var data = unsafe.Add(p, d.offset)
	if d.isPrt {
		*(**int64)(data) = &val
	} else {
		*(*int64)(data) = val
	}
	return buf, nil
}

func (d *Int64Descriptor) IsEmpty(p unsafe.Pointer) bool {
	if d.isPrt {
		return *(**int64)(unsafe.Add(p, d.offset)) == nil
	} else {
		return *(*int64)(unsafe.Add(p, d.offset)) == 0
	}
}

func (d *Int64Descriptor) Encode(buf []byte, p unsafe.Pointer, id uint16) []byte {
	var val int64
	if d.isPrt {
		ptr := *(**int64)(unsafe.Add(p, d.offset))
		if ptr == nil {
			return buf
		}
		val = *ptr
	} else {
		val = *(*int64)(unsafe.Add(p, d.offset))
	}
	if val == 0 {
		return buf
	}
	buf = WriterTypeId(buf, TInt, id, LengthInt(val))
	return WriterInt64(buf, val)
}

type ListDescriptor[T any] struct {
	offset uintptr
	desc   Descriptor
}

func NewListDescriptor[T any](offset uintptr, desc Descriptor) *ListDescriptor[T] {
	return &ListDescriptor[T]{offset: offset, desc: desc}
}

func (d *ListDescriptor[T]) IsEmpty(p unsafe.Pointer) bool {
	var data = unsafe.Add(p, d.offset)
	v := *(*[]T)(data)
	if len(v) == 0 {
		return true
	}
	return false
}

func (d *ListDescriptor[T]) Encode(buf []byte, p unsafe.Pointer, id uint16) []byte {
	var data = unsafe.Add(p, d.offset)
	list := *(*[]T)(data)
	count := len(list)
	if count == 0 {
		return buf
	}
	buf = WriterTypeId(buf, TList, id, LengthUint(uint64(count)))
	buf = WriterUint64(buf, uint64(count))
	for i := 0; i < count; i++ {
		buf = d.desc.Encode(buf, unsafe.Pointer(&list[i]), 0)
	}
	return buf
}

func (d *ListDescriptor[T]) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8) ([]byte, error) {
	if typ != TList {
		return nil, errors.New("invalid list type")
	}
	var count uint64
	var err error

	count, buf = DecodeUint64(buf, valueLen)

	list := make([]T, count)
	for i := uint64(0); i < count; i++ {
		typ, _, valueLen, buf = Reader(buf)
		buf, err = d.desc.Decode(buf, unsafe.Pointer(&list[i]), typ, valueLen)
		if err != nil {
			return nil, err
		}
	}
	var data = unsafe.Add(p, d.offset)
	*(*[]T)(data) = list
	return buf, nil
}

type MapDescriptor[K comparable, V any] struct {
	offset    uintptr
	keyDesc   Descriptor
	valueDesc Descriptor
}

func NewMapDescriptor[K comparable, V any](offset uintptr, keyDesc Descriptor, valueDesc Descriptor) *MapDescriptor[K, V] {
	return &MapDescriptor[K, V]{offset: offset, keyDesc: keyDesc, valueDesc: valueDesc}
}

func (d *MapDescriptor[K, V]) IsEmpty(p unsafe.Pointer) bool {
	var data = unsafe.Add(p, d.offset)
	v := *(*map[K]V)(data)
	if len(v) == 0 {
		return true
	}
	return false
}

func (d *MapDescriptor[K, V]) Encode(buf []byte, p unsafe.Pointer, id uint16) []byte {
	var data = unsafe.Add(p, d.offset)
	m := *(*map[K]V)(data)
	count := len(m)
	if count == 0 {
		return buf
	}
	buf = WriterTypeId(buf, TMap, id, LengthUint(uint64(count)))
	buf = WriterUint64(buf, uint64(count))
	for k, v := range m {
		buf = d.keyDesc.Encode(buf, unsafe.Pointer(&k), 0)
		buf = d.valueDesc.Encode(buf, unsafe.Pointer(&v), 0)
	}
	return buf
}

func (d *MapDescriptor[K, V]) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8) ([]byte, error) {
	if typ != TMap {
		return nil, errors.New("invalid map type")
	}
	var count uint64
	var err error

	count, buf = DecodeUint64(buf, valueLen)
	m := make(map[K]V, count)
	for i := uint64(0); i < count; i++ {
		var k K
		var v V
		typ, _, valueLen, buf = Reader(buf)
		buf, err = d.keyDesc.Decode(buf, unsafe.Pointer(&k), typ, valueLen)
		if err != nil {
			return nil, err
		}
		typ, _, valueLen, buf = Reader(buf)
		buf, err = d.valueDesc.Decode(buf, unsafe.Pointer(&v), typ, valueLen)
		if err != nil {
			return nil, err
		}
		m[k] = v
	}
	*(*map[K]V)(unsafe.Add(p, d.offset)) = m
	return buf, nil
}

type StringDescriptor struct {
	offset uintptr
}

func NewStringDescriptor(offset uintptr) *StringDescriptor {
	return &StringDescriptor{offset: offset}
}

func (d *StringDescriptor) IsEmpty(p unsafe.Pointer) bool {
	var data = unsafe.Add(p, d.offset)
	v := *(*string)(data)
	if len(v) == 0 {
		return true
	}
	return false
}

func (d *StringDescriptor) Encode(buf []byte, p unsafe.Pointer, id uint16) []byte {
	var data = unsafe.Add(p, d.offset)
	s := *(*string)(data)
	if len(s) == 0 {
		return buf
	}
	size := len(s)
	buf = WriterTypeId(buf, TBytes, id, LengthUint(uint64(size)))
	buf = WriterUint64(buf, uint64(size))
	return append(buf, s...)
}

func (d *StringDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8) ([]byte, error) {
	if typ != TBytes {
		return nil, errors.New("invalid string type")
	}

	size, buf := DecodeUint64(buf, valueLen)
	*((*string)(unsafe.Add(p, d.offset))) = string(buf[:size])
	return buf[size:], nil
}
