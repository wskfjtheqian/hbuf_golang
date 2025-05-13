package hbuf

import (
	"errors"
	"reflect"
	"sort"
	"unsafe"
)

type Descriptor interface {
	GetValue(p unsafe.Pointer, tag string) unsafe.Pointer
	SetValue(p unsafe.Pointer, tag string) unsafe.Pointer
	Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte
	Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error)
	SetTag(tags map[string]bool)
}

func listToSet(list []string) map[string]bool {
	set := make(map[string]bool, len(list))
	for _, str := range list {
		set[str] = true
	}
	return set
}

func NewDataDescriptor(offset uintptr, isPtr bool, typ reflect.Type, fieldMap map[uint16]Descriptor, tags ...string) Descriptor {
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
		tags:     listToSet(tags),
	}
}

func CloneDataDescriptor(d Data, offset uintptr, isPtr bool, tags ...string) Descriptor {
	var desc = d.Descriptors().(*DataDescriptor)
	return &DataDescriptor{
		offset:   offset,
		fieldMap: desc.fieldMap,
		fields:   desc.fields,
		ids:      desc.ids,
		isPtr:    isPtr,
		typ:      desc.typ,
		tags:     listToSet(tags),
	}
}

type DataDescriptor struct {
	offset   uintptr
	fieldMap map[uint16]Descriptor
	fields   []Descriptor
	ids      []uint16
	isPtr    bool
	typ      reflect.Type
	tags     map[string]bool
}

func (d *DataDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return p
	}
	return nil
}

func (d *DataDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *DataDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 {
		if _, ok := d.tags[tag]; !ok {
			return nil
		}
	}
	ptr := unsafe.Add(p, d.offset)
	if d.isPtr {
		ptr = *(*unsafe.Pointer)(ptr)
	}
	return ptr
}

func (d *DataDescriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	if p == nil {
		if null {
			buf = WriterTypeId(buf, TData, id, LengthUint(0))
			buf = WriterUint64(buf, 0)
		}
		return buf
	}

	count := uint64(0)
	for _, desc := range d.fields {
		if desc.GetValue(p, tag) != nil {
			count++
		}
	}
	buf = WriterTypeId(buf, TData, id, LengthUint(count))
	buf = WriterUint64(buf, count)

	for i, desc := range d.fields {
		buf = desc.Encode(buf, desc.GetValue(p, tag), d.ids[i], false, tag)
	}
	return buf
}

func (d *DataDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TData {
		return nil, errors.New("invalid data type")
	}

	count, buf := DecodeUint64(buf, valueLen)
	if count == 0 {
		return buf, nil
	}

	var err error
	var id uint16

	if p != nil {
		data := reflect.New(d.typ)
		ptr := data.UnsafePointer()

		for i := uint64(0); i < count; i++ {
			typ, id, valueLen, buf = Reader(buf)

			if field, ok := d.fieldMap[id]; ok {
				buf, err = field.Decode(buf, field.SetValue(ptr, tag), typ, valueLen, tag)
				if err != nil {
					return nil, err
				}
			}
		}
		*(*unsafe.Pointer)(unsafe.Add(p, d.offset)) = ptr
	} else {
		for i := uint64(0); i < count; i++ {
			typ, id, valueLen, buf = Reader(buf)

			if field, ok := d.fieldMap[id]; ok {
				buf, err = field.Decode(buf, nil, typ, valueLen, tag)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return buf, nil
}

func NewListDescriptor[T any](offset uintptr, desc Descriptor, tags ...string) Descriptor {
	tagMap := listToSet(tags)
	desc.SetTag(tagMap)
	return &ListDescriptor[T]{
		offset: offset,
		desc:   desc,
		tags:   tagMap,
	}
}

type ListDescriptor[T any] struct {
	offset uintptr
	desc   Descriptor
	tags   map[string]bool
}

func (d *ListDescriptor[T]) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return p
	}
	return nil
}

func (d *ListDescriptor[T]) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *ListDescriptor[T]) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 {
		if _, ok := d.tags[tag]; !ok {
			return nil
		}
	}
	var ptr = unsafe.Add(p, d.offset)
	v := *(*[]T)(ptr)
	if len(v) == 0 {
		return nil
	}
	return ptr
}

func (d *ListDescriptor[T]) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	if p == nil && !null {
		return buf
	}

	list := *(*[]T)(p)
	count := len(list)
	buf = WriterTypeId(buf, TList, id, LengthUint(uint64(count)))
	buf = WriterUint64(buf, uint64(count))

	for i := 0; i < count; i++ {
		buf = d.desc.Encode(buf, d.desc.GetValue(unsafe.Pointer(&list[i]), tag), 0, true, tag)
	}
	return buf
}

func (d *ListDescriptor[T]) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TList {
		return nil, errors.New("invalid list type")
	}
	var count uint64
	var err error

	count, buf = DecodeUint64(buf, valueLen)
	if p != nil {
		list := make([]T, count)
		for i := uint64(0); i < count; i++ {
			typ, _, valueLen, buf = Reader(buf)
			buf, err = d.desc.Decode(buf, d.desc.SetValue(unsafe.Pointer(&list[i]), tag), typ, valueLen, tag)
			if err != nil {
				return nil, err
			}
		}
		var data = unsafe.Add(p, d.offset)
		*(*[]T)(data) = list
	} else {
		for i := uint64(0); i < count; i++ {
			typ, _, valueLen, buf = Reader(buf)
			buf, err = d.desc.Decode(buf, nil, typ, valueLen, tag)
			if err != nil {
				return nil, err
			}
		}
	}
	return buf, nil
}

func NewMapDescriptor[K comparable, V any](offset uintptr, keyDesc Descriptor, valueDesc Descriptor, tags ...string) Descriptor {
	tagMap := listToSet(tags)
	keyDesc.SetTag(tagMap)
	valueDesc.SetTag(tagMap)

	return &MapDescriptor[K, V]{
		offset:    offset,
		keyDesc:   keyDesc,
		valueDesc: valueDesc,
		tags:      tagMap,
	}
}

type MapDescriptor[K comparable, V any] struct {
	offset    uintptr
	keyDesc   Descriptor
	valueDesc Descriptor
	tags      map[string]bool
}

func (d *MapDescriptor[K, V]) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return p
	}
	return nil
}

func (d *MapDescriptor[K, V]) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *MapDescriptor[K, V]) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 {
		if _, ok := d.tags[tag]; !ok {
			return nil
		}
	}
	ptr := unsafe.Add(p, d.offset)
	v := *(*map[K]V)(ptr)
	if len(v) == 0 {
		return nil
	}
	return ptr
}

func (d *MapDescriptor[K, V]) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	if p == nil {
		if !null {
			return buf
		}
	}

	m := *(*map[K]V)(p)
	count := len(m)
	buf = WriterTypeId(buf, TMap, id, LengthUint(uint64(count)))
	buf = WriterUint64(buf, uint64(count))

	for k, v := range m {
		buf = d.keyDesc.Encode(buf, d.keyDesc.GetValue(unsafe.Pointer(&k), tag), 0, true, tag)
		buf = d.valueDesc.Encode(buf, d.valueDesc.GetValue(unsafe.Pointer(&v), tag), 0, true, tag)
	}
	return buf
}

func (d *MapDescriptor[K, V]) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TMap {
		return nil, errors.New("invalid map type")
	}
	var count uint64
	var err error

	count, buf = DecodeUint64(buf, valueLen)
	if p != nil {
		m := make(map[K]V, count)
		for i := uint64(0); i < count; i++ {
			var k K
			var v V
			typ, _, valueLen, buf = Reader(buf)
			buf, err = d.keyDesc.Decode(buf, d.keyDesc.SetValue(unsafe.Pointer(&k), tag), typ, valueLen, tag)
			if err != nil {
				return nil, err
			}
			typ, _, valueLen, buf = Reader(buf)
			buf, err = d.valueDesc.Decode(buf, d.keyDesc.SetValue(unsafe.Pointer(&v), tag), typ, valueLen, tag)
			if err != nil {
				return nil, err
			}
			m[k] = v
		}
		*(*map[K]V)(unsafe.Add(p, d.offset)) = m
	} else {
		for i := uint64(0); i < count; i++ {
			typ, _, valueLen, buf = Reader(buf)
			buf, err = d.keyDesc.Decode(buf, nil, typ, valueLen, tag)
			if err != nil {
				return nil, err
			}
			typ, _, valueLen, buf = Reader(buf)
			buf, err = d.valueDesc.Decode(buf, nil, typ, valueLen, tag)
			if err != nil {
				return nil, err
			}
		}
	}
	return buf, nil
}

func NewStringDescriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &StringDescriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type StringDescriptor struct {
	offset uintptr
	tags   map[string]bool
	isPrt  bool
}

func (d *StringDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return p
	}
	return nil
}

func (d *StringDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *StringDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 {
		if _, ok := d.tags[tag]; !ok {
			return nil
		}
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	if len(*(*string)(ptr)) == 0 {
		return nil
	}
	return ptr
}

func (d *StringDescriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val string
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*string)(p)
	}
	size := len(val)

	buf = WriterTypeId(buf, TBytes, id, LengthUint(uint64(size)))
	buf = WriterUint64(buf, uint64(size))
	return append(buf, val...)
}

func (d *StringDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TBytes {
		return nil, errors.New("invalid string type")
	}

	size, buf := DecodeUint64(buf, valueLen)
	if p != nil {
		if d.isPrt {
			val := string(buf[:size])
			*((**string)(unsafe.Add(p, d.offset))) = &val
		} else {
			*((*string)(unsafe.Add(p, d.offset))) = string(buf[:size])
		}
	}
	return buf[size:], nil
}

func NewInt64Descriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &Int64Descriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type Int64Descriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *Int64Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return p
	}
	return nil
}

func (d *Int64Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Int64Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 {
		if _, ok := d.tags[tag]; !ok {
			return nil
		}
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	if *(*int64)(ptr) == 0 {
		return nil
	}
	return ptr
}

func (d *Int64Descriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val int64
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*int64)(p)
	}

	buf = WriterTypeId(buf, TInt, id, LengthInt(val))
	return WriterInt64(buf, val)
}

func (d *Int64Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	val, buf := DecodeInt64(buf, valueLen)
	if p != nil {
		if d.isPrt {
			*(**int64)(unsafe.Add(p, d.offset)) = &val
		} else {
			*(*int64)(unsafe.Add(p, d.offset)) = val
		}
	}
	return buf, nil
}
