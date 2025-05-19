package hbuf

import (
	"errors"
	"github.com/shopspring/decimal"
	"reflect"
	"sort"
	"time"
	"unsafe"
)

type Descriptor interface {
	IsEmpty(p unsafe.Pointer, tag string) bool
	GetValue(p unsafe.Pointer, tag string) unsafe.Pointer
	SetValue(p unsafe.Pointer, tag string) unsafe.Pointer
	Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte
	Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error)
	SetTag(tags map[string]bool)
}

func listToSet(list []string) map[string]bool {
	set := make(map[string]bool, len(list))
	for _, str := range list {
		set[str] = true
	}
	return set
}

func NewDataDescriptor(offset uintptr, isPtr bool, typ reflect.Type, extendMap map[uint16]Descriptor, fieldMap map[uint16]Descriptor, tags ...string) Descriptor {
	var extends []Descriptor
	extendIds := make([]uint16, 0, len(tags))
	for id, _ := range extendMap {
		extendIds = append(extendIds, id)
	}
	sort.Slice(extendIds, func(i, j int) bool { return extendIds[i] < extendIds[j] })
	for _, id := range extendIds {
		extends = append(extends, extendMap[id])
	}

	var fields []Descriptor
	fieldIds := make([]uint16, 0, len(tags))
	for id, _ := range fieldMap {
		fieldIds = append(fieldIds, id)
	}
	sort.Slice(fieldIds, func(i, j int) bool { return fieldIds[i] < fieldIds[j] })
	for _, id := range fieldIds {
		fields = append(fields, fieldMap[id])
	}
	return &DataDescriptor{
		offset:    offset,
		fieldMap:  fieldMap,
		fields:    fields,
		fieldIds:  fieldIds,
		extendMap: extendMap,
		extends:   extends,
		extendIds: extendIds,
		isPtr:     isPtr,
		typ:       typ,
		tags:      listToSet(tags),
	}
}

func CloneDataDescriptor(d Data, offset uintptr, isPtr bool, tags ...string) Descriptor {
	var desc = d.Descriptors().(*DataDescriptor)
	return &DataDescriptor{
		offset:   offset,
		fieldMap: desc.fieldMap,
		fields:   desc.fields,
		fieldIds: desc.fieldIds,
		isPtr:    isPtr,
		typ:      desc.typ,
		tags:     listToSet(tags),
	}
}

type DataDescriptor struct {
	offset    uintptr
	fieldMap  map[uint16]Descriptor
	fields    []Descriptor
	fieldIds  []uint16
	isPtr     bool
	typ       reflect.Type
	tags      map[string]bool
	extendMap map[uint16]Descriptor
	extends   []Descriptor
	extendIds []uint16
}

func (d *DataDescriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	return d.GetValue(p, tag) == nil
}

func (d *DataDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *DataDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}
	ptr := unsafe.Add(p, d.offset)
	if d.isPtr {
		ptr = *(*unsafe.Pointer)(ptr)
	}
	return ptr
}

func (d *DataDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *DataDescriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil {
		if id == nil {
			return WriterType(buf, TData, 0, 1)
		} else {
			return buf
		}
	}

	count := uint64(0)
	for _, desc := range d.fields {
		if !desc.IsEmpty(p, tag) {
			count++
		}
	}
	if id == nil {
		buf = WriterType(buf, TData, 0, LengthUint(count))
	} else {
		buf = WriterType(buf, TData, LengthId(*id), LengthUint(count))
		buf = WriterId(buf, *id)
	}
	buf = EncodeUint64(buf, count)
	for i, desc := range d.fields {
		buf = desc.Encode(buf, desc.GetValue(p, tag), &d.fieldIds[i], tag)
	}

	count = uint64(len(d.extends))
	if count == 0 {
		buf = WriterType(buf, TInt, 0, LengthUint(count))
	} else {
		buf = WriterType(buf, TInt, 1, LengthUint(count))
		buf = EncodeUint64(buf, count)
	}
	for i, extend := range d.extends {
		buf = extend.Encode(buf, extend.GetValue(p, tag), &d.extendIds[i], tag)
	}
	return buf
}

func (d *DataDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TData {
		return nil, errors.New("invalid data type")
	}

	count, buf := DecodeUint64(buf, valueLen)
	if count == 0 {
		return buf, nil
	}

	var err error
	ptr := p
	if p != nil {
		if d.isPtr {
			data := reflect.New(d.typ.Elem())
			ptr = data.UnsafePointer()
			*(**unsafe.Pointer)(p) = (*unsafe.Pointer)(ptr)
		}
	}

	var idLen uint8
	var id uint16
	for i := uint64(0); i < count; i++ {
		typ, idLen, valueLen, buf = DecodeType(buf)
		id, buf = DecodeId(buf, idLen)

		if field, ok := d.fieldMap[id]; ok {
			buf, err = field.Decode(buf, field.SetValue(ptr, tag), typ, true, valueLen, tag)
			if err != nil {
				return nil, err
			}
		}
	}

	typ, idLen, valueLen, buf = DecodeType(buf)
	if idLen > 0 {
		count, buf = DecodeUint64(buf, valueLen)
		for i := uint64(0); i < count; i++ {
			typ, idLen, valueLen, buf = DecodeType(buf)
			id, buf = DecodeId(buf, idLen)

			if extend, ok := d.extendMap[id]; ok {
				buf, err = extend.Decode(buf, extend.SetValue(ptr, tag), typ, false, valueLen, tag)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return buf, nil
}

func NewListDescriptor[T any](offset uintptr, desc Descriptor, isPtr bool, tags ...string) Descriptor {
	tagMap := listToSet(tags)
	desc.SetTag(tagMap)
	return &ListDescriptor[T]{
		offset: offset,
		desc:   desc,
		tags:   tagMap,
		isPtr:  isPtr,
	}
}

type ListDescriptor[T any] struct {
	offset uintptr
	desc   Descriptor
	tags   map[string]bool
	isPtr  bool
}

func (d *ListDescriptor[T]) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}
	return len(*(*[]T)(unsafe.Add(p, d.offset))) == 0
}

func (d *ListDescriptor[T]) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *ListDescriptor[T]) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}
	var ptr = unsafe.Add(p, d.offset)
	if len(*(*[]T)(ptr)) == 0 {
		return nil
	}
	return ptr
}

func (d *ListDescriptor[T]) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *ListDescriptor[T]) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil {
		if id == nil {
			return WriterType(buf, TList, 0, 1)
		} else {
			return buf
		}
	}

	list := *(*[]T)(p)
	count := len(list)
	if id == nil {
		buf = WriterType(buf, TList, 1, LengthUint(uint64(count)))
	} else {
		buf = WriterType(buf, TList, LengthId(*id), LengthUint(uint64(count)))
		buf = WriterId(buf, *id)
	}

	buf = EncodeUint64(buf, uint64(count))
	for i := 0; i < count; i++ {
		buf = d.desc.Encode(buf, d.desc.GetValue(unsafe.Pointer(&list[i]), tag), nil, tag)
	}
	return buf
}

func (d *ListDescriptor[T]) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TList {
		return nil, errors.New("invalid list type")
	}
	var count uint64
	var err error
	var idLen uint8

	count, buf = DecodeUint64(buf, valueLen)
	if p != nil {
		list := make([]T, count)
		for i := uint64(0); i < count; i++ {
			typ, idLen, valueLen, buf = DecodeType(buf)
			if idLen > 0 {
				buf, err = d.desc.Decode(buf, d.desc.SetValue(unsafe.Pointer(&list[i]), tag), typ, idLen == 1, valueLen, tag)
				if err != nil {
					return nil, err
				}
			}
		}
		*(*[]T)(p) = list
	} else {
		for i := uint64(0); i < count; i++ {
			typ, idLen, valueLen, buf = DecodeType(buf)
			if idLen > 0 {
				buf, err = d.desc.Decode(buf, nil, typ, idLen == 1, valueLen, tag)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return buf, nil
}

func NewMapDescriptor[K comparable, V any](offset uintptr, keyDesc Descriptor, valueDesc Descriptor, isPtr bool, tags ...string) Descriptor {
	tagMap := listToSet(tags)
	keyDesc.SetTag(tagMap)
	valueDesc.SetTag(tagMap)

	return &MapDescriptor[K, V]{
		offset:    offset,
		keyDesc:   keyDesc,
		valueDesc: valueDesc,
		tags:      tagMap,
		isPtr:     isPtr,
	}
}

type MapDescriptor[K comparable, V any] struct {
	offset    uintptr
	keyDesc   Descriptor
	valueDesc Descriptor
	tags      map[string]bool
	isPtr     bool
}

func (d *MapDescriptor[K, V]) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}
	return len(*(*map[K]V)(unsafe.Add(p, d.offset))) == 0
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

func (d *MapDescriptor[K, V]) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *MapDescriptor[K, V]) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil {
		if id == nil {
			return WriterType(buf, TMap, 0, 1)
		} else {
			return buf
		}
	}

	m := *(*map[K]V)(p)
	count := len(m)
	if id == nil {
		buf = WriterType(buf, TMap, 1, LengthUint(uint64(count)))
	} else {
		buf = WriterType(buf, TMap, LengthId(*id), LengthUint(uint64(count)))
		buf = WriterId(buf, *id)
	}
	buf = EncodeUint64(buf, uint64(count))

	for k, v := range m {
		buf = d.keyDesc.Encode(buf, d.keyDesc.GetValue(unsafe.Pointer(&k), tag), nil, tag)
		buf = d.valueDesc.Encode(buf, d.valueDesc.GetValue(unsafe.Pointer(&v), tag), nil, tag)
	}
	return buf
}

func (d *MapDescriptor[K, V]) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TMap {
		return nil, errors.New("invalid map type")
	}

	var count uint64
	var err error
	var idLen uint8

	count, buf = DecodeUint64(buf, valueLen)
	if p != nil {
		m := make(map[K]V, count)
		for i := uint64(0); i < count; i++ {
			var k K
			var v V
			typ, idLen, valueLen, buf = DecodeType(buf)
			buf, err = d.keyDesc.Decode(buf, d.keyDesc.SetValue(unsafe.Pointer(&k), tag), typ, true, valueLen, tag)
			if err != nil {
				return nil, err
			}
			typ, idLen, valueLen, buf = DecodeType(buf)
			if idLen > 0 {
				buf, err = d.valueDesc.Decode(buf, d.keyDesc.SetValue(unsafe.Pointer(&v), tag), typ, idLen == 1, valueLen, tag)
				if err != nil {
					return nil, err
				}
			}
			m[k] = v
		}
		*(*map[K]V)(p) = m
	} else {
		for i := uint64(0); i < count; i++ {
			typ, idLen, valueLen, buf = DecodeType(buf)
			buf, err = d.keyDesc.Decode(buf, nil, typ, true, valueLen, tag)
			if err != nil {
				return nil, err
			}
			typ, idLen, valueLen, buf = DecodeType(buf)
			if idLen > 0 {
				buf, err = d.valueDesc.Decode(buf, nil, typ, idLen == 1, valueLen, tag)
				if err != nil {
					return nil, err
				}
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

func (d *StringDescriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return len(*(*string)(ptr)) == 0
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
	return ptr
}

func (d *StringDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *StringDescriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || len(*(*string)(p)) == 0 {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TBytes, 2, 1)
		} else {
			return WriterType(buf, TBytes, 0, 1)
		}
	}

	val := *(*string)(p)
	size := uint64(len(val))
	if id == nil {
		buf = WriterType(buf, TBytes, 1, LengthUint(size))
	} else {
		buf = WriterType(buf, TBytes, LengthId(*id), LengthUint(size))
		buf = WriterId(buf, *id)
	}
	buf = EncodeUint64(buf, size)
	return append(buf, val...)
}

func (d *StringDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TBytes {
		return nil, errors.New("invalid string type")
	}

	var size uint64
	var val string
	if valRead {
		size, buf = DecodeUint64(buf, valueLen)
		val = string(buf[:size])
		buf = buf[size:]
	}
	if p != nil {
		if d.isPrt {
			*(**string)(p) = &val
		} else {
			*(*string)(p) = val
		}
	}
	return buf, nil
}

func NewBytesDescriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &BytesDescriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type BytesDescriptor struct {
	offset uintptr
	tags   map[string]bool
	isPrt  bool
}

func (d *BytesDescriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return len(*(*[]byte)(ptr)) == 0
}

func (d *BytesDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *BytesDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	return ptr
}

func (d *BytesDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *BytesDescriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || len(*(*[]byte)(p)) == 0 {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TBytes, 2, 1)
		} else {
			return WriterType(buf, TBytes, 0, 1)
		}
	}

	val := *(*[]byte)(p)
	size := uint64(len(val))
	if id == nil {
		buf = WriterType(buf, TBytes, 1, LengthUint(size))
	} else {
		buf = WriterType(buf, TBytes, LengthId(*id), LengthUint(size))
		buf = WriterId(buf, *id)
	}
	buf = EncodeUint64(buf, size)
	return append(buf, val...)
}

func (d *BytesDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TBytes {
		return nil, errors.New("invalid string type")
	}

	var size uint64
	var val string
	if valRead {
		size, buf = DecodeUint64(buf, valueLen)
		val = string(buf[:size])
		buf = buf[size:]
	}
	if p != nil {
		if d.isPrt {
			*(**string)(p) = &val
		} else {
			*(*string)(p) = val
		}
	}
	return buf, nil
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

func (d *Int64Descriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	if p == nil {
		return true
	} else if *(*int64)(ptr) == 0 {
		return !d.isPrt
	}
	return false
}

func (d *Int64Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Int64Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *Int64Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Int64Descriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	var val int64
	if id != nil {
		if (d.isPrt && p == nil) || (!d.isPrt && (p == nil || *(*int64)(p) == 0)) {
			return buf
		}

		val = *(*int64)(p)
		buf = WriterType(buf, TInt, LengthId(*id), LengthInt(val))
		buf = WriterId(buf, *id)
	} else {
		if p == nil {
			if !d.isPrt {
				return buf
			}
			return WriterType(buf, TInt, 0, 1)
		}
		if d.isPrt || *(*int64)(p) != 0 {
			val = *(*int64)(p)
			buf = WriterType(buf, TInt, 1, LengthInt(val))
		}
	}
	return WriterInt64(buf, val)
}

func (d *Int64Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid int64 type")
	}
	var val int64
	if valRead {
		val, buf = DecodeInt64(buf, valueLen)
	}
	if p != nil {
		if d.isPrt {
			*(**int64)(p) = &val
		} else {
			*(*int64)(p) = val
		}
	}
	return buf, nil
}

func NewInt32Descriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &Int32Descriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type Int32Descriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *Int32Descriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return *(*int32)(ptr) == 0
}

func (d *Int32Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Int32Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *Int32Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Int32Descriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || *(*int32)(p) == 0 {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TInt, 2, 1)
		} else {
			return WriterType(buf, TInt, 0, 1)
		}
	}

	val := *(*int32)(p)
	if id == nil {
		buf = WriterType(buf, TInt, 1, LengthInt(int64(val)))
	} else {
		buf = WriterType(buf, TInt, LengthId(*id), LengthInt(int64(val)))
		buf = WriterId(buf, *id)
	}
	return WriterInt64(buf, int64(val))
}

func (d *Int32Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid int32 type")
	}
	var val int64
	if valRead {
		val, buf = DecodeInt64(buf, valueLen)
	}
	if p != nil {
		if d.isPrt {
			temp := int32(val)
			*(**int32)(p) = &temp
		} else {
			*(*int32)(p) = int32(val)
		}
	}
	return buf, nil
}

func NewInt16Descriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &Int16Descriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type Int16Descriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *Int16Descriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return *(*int16)(ptr) == 0
}

func (d *Int16Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Int16Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *Int16Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Int16Descriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || *(*int16)(p) == 0 {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TInt, 2, 1)
		} else {
			return WriterType(buf, TInt, 0, 1)
		}
	}

	val := *(*int16)(p)
	if id == nil {
		buf = WriterType(buf, TInt, 1, LengthInt(int64(val)))
	} else {
		buf = WriterType(buf, TInt, LengthId(*id), LengthInt(int64(val)))
		buf = WriterId(buf, *id)
	}
	return WriterInt64(buf, int64(val))
}

func (d *Int16Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid int16 type")
	}
	var val int64
	if valRead {
		val, buf = DecodeInt64(buf, valueLen)
	}
	if p != nil {
		if d.isPrt {
			temp := int16(val)
			*(**int16)(p) = &temp
		} else {
			*(*int16)(p) = int16(val)
		}
	}
	return buf, nil
}

func NewInt8Descriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &Int8Descriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type Int8Descriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *Int8Descriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return *(*int8)(ptr) == 0
}

func (d *Int8Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Int8Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *Int8Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Int8Descriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || *(*int8)(p) == 0 {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TInt, 2, 1)
		} else {
			return WriterType(buf, TInt, 0, 1)
		}
	}

	val := *(*int8)(p)
	if id == nil {
		buf = WriterType(buf, TInt, 1, LengthInt(int64(val)))
	} else {
		buf = WriterType(buf, TInt, LengthId(*id), LengthInt(int64(val)))
		buf = WriterId(buf, *id)
	}
	return WriterInt64(buf, int64(val))
}

func (d *Int8Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid int8 type")
	}
	var val int64
	if valRead {
		val, buf = DecodeInt64(buf, valueLen)
	}
	if p != nil {
		if d.isPrt {
			temp := int8(val)
			*(**int8)(p) = &temp
		} else {
			*(*int8)(p) = int8(val)
		}
	}
	return buf, nil
}

func NewUint64Descriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &Uint64Descriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type Uint64Descriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *Uint64Descriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return *(*uint64)(ptr) == 0
}

func (d *Uint64Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Uint64Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *Uint64Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Uint64Descriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || *(*uint64)(p) == 0 {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TInt, 2, 1)
		} else {
			return WriterType(buf, TInt, 0, 1)
		}
	}

	val := *(*uint64)(p)
	if id == nil {
		buf = WriterType(buf, TInt, 1, LengthUint(val))
	} else {
		buf = WriterType(buf, TInt, LengthId(*id), LengthUint(val))
		buf = WriterId(buf, *id)
	}
	return EncodeUint64(buf, val)
}

func (d *Uint64Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid uint64 type")
	}
	var val uint64
	if valRead {
		val, buf = DecodeUint64(buf, valueLen)
	}
	if p != nil {
		if d.isPrt {
			*(**uint64)(p) = &val
		} else {
			*(*uint64)(p) = val
		}
	}
	return buf, nil
}

func NewUint32Descriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &Uint32Descriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type Uint32Descriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *Uint32Descriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return *(*uint32)(ptr) == 0
}

func (d *Uint32Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Uint32Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *Uint32Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Uint32Descriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || *(*uint32)(p) == 0 {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TInt, 2, 1)
		} else {
			return WriterType(buf, TInt, 0, 1)
		}
	}

	val := *(*uint32)(p)
	if id == nil {
		buf = WriterType(buf, TInt, 1, LengthUint(uint64(val)))
	} else {
		buf = WriterType(buf, TInt, LengthId(*id), LengthUint(uint64(val)))
		buf = WriterId(buf, *id)
	}
	return EncodeUint64(buf, uint64(val))
}

func (d *Uint32Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid uint32 type")
	}
	var val uint64
	if valRead {
		val, buf = DecodeUint64(buf, valueLen)
	}
	if p != nil {
		if d.isPrt {
			temp := uint32(val)
			*(**uint32)(p) = &temp
		} else {
			*(*uint32)(p) = uint32(val)
		}
	}
	return buf, nil
}

func NewUint16Descriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &Uint16Descriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type Uint16Descriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *Uint16Descriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return *(*uint16)(ptr) == 0
}

func (d *Uint16Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Uint16Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *Uint16Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Uint16Descriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || *(*uint16)(p) == 0 {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TInt, 2, 1)
		} else {
			return WriterType(buf, TInt, 0, 1)
		}
	}

	val := *(*uint16)(p)
	if id == nil {
		buf = WriterType(buf, TInt, 1, LengthUint(uint64(val)))
	} else {
		buf = WriterType(buf, TInt, LengthId(*id), LengthUint(uint64(val)))
		buf = WriterId(buf, *id)
	}
	return EncodeUint64(buf, uint64(val))
}

func (d *Uint16Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid uint16 type")
	}
	var val uint64
	if valRead {
		val, buf = DecodeUint64(buf, valueLen)
	}
	if p != nil {
		if d.isPrt {
			temp := uint16(val)
			*(**uint16)(p) = &temp
		} else {
			*(*uint16)(p) = uint16(val)
		}
	}
	return buf, nil
}

func NewUint8Descriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &Uint8Descriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type Uint8Descriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *Uint8Descriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return *(*uint8)(ptr) == 0
}

func (d *Uint8Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Uint8Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *Uint8Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Uint8Descriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || *(*uint8)(p) == 0 {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TInt, 2, 1)
		} else {
			return WriterType(buf, TInt, 0, 1)
		}
	}

	val := *(*uint8)(p)
	if id == nil {
		buf = WriterType(buf, TInt, 1, LengthUint(uint64(val)))
	} else {
		buf = WriterType(buf, TInt, LengthId(*id), LengthUint(uint64(val)))
		buf = WriterId(buf, *id)
	}
	return EncodeUint64(buf, uint64(val))
}

func (d *Uint8Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid uint8 type")
	}
	var val uint64
	if valRead {
		val, buf = DecodeUint64(buf, valueLen)
	}
	if p != nil {
		if d.isPrt {
			temp := uint8(val)
			*(**uint8)(p) = &temp
		} else {
			*(*uint8)(p) = uint8(val)
		}
	}
	return buf, nil
}

func NewDoubleDescriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &DoubleDescriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type DoubleDescriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *DoubleDescriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return *(*uint64)(ptr) == 0
}

func (d *DoubleDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *DoubleDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *DoubleDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *DoubleDescriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || *(*uint64)(p) == 0 {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TInt, 2, 1)
		} else {
			return WriterType(buf, TInt, 0, 1)
		}
	}

	val := *(*uint64)(p)
	if id == nil {
		buf = WriterType(buf, TInt, 1, LengthUint(val))
	} else {
		buf = WriterType(buf, TInt, LengthId(*id), LengthUint(val))
		buf = WriterId(buf, *id)
	}
	return EncodeUint64(buf, val)
}

func (d *DoubleDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid double type")
	}
	var val uint64
	if valRead {
		val, buf = DecodeUint64(buf, valueLen)
	}
	if p != nil {
		if d.isPrt {
			*(**uint64)(p) = &val
		} else {
			*(*uint64)(p) = val
		}
	}
	return buf, nil
}

func NewFloatDescriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &FloatDescriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type FloatDescriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *FloatDescriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return *(*uint32)(ptr) == 0
}

func (d *FloatDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *FloatDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *FloatDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *FloatDescriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || *(*uint32)(p) == 0 {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TInt, 2, 1)
		} else {
			return WriterType(buf, TInt, 0, 1)
		}
	}

	val := *(*uint32)(p)
	if id == nil {
		buf = WriterType(buf, TInt, 1, LengthUint(uint64(val)))
	} else {
		buf = WriterType(buf, TInt, LengthId(*id), LengthUint(uint64(val)))
		buf = WriterId(buf, *id)
	}
	return EncodeUint64(buf, uint64(val))
}

func (d *FloatDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid float type")
	}
	var val uint64
	if valRead {
		val, buf = DecodeUint64(buf, valueLen)
	}
	if p != nil {
		if d.isPrt {
			temp := uint32(val)
			*(**uint32)(p) = &temp
		} else {
			*(*uint32)(p) = uint32(val)
		}
	}
	return buf, nil
}

func NewBoolDescriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &BoolDescriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type BoolDescriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *BoolDescriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}

	if d.isPrt {
		if p == nil {
			return true
		} else if *(*bool)(ptr) {
			return false
		} else {
			return false
		}
	} else {
		if p == nil {
			return true
		} else if *(*bool)(ptr) {
			return false
		} else {
			return true
		}
	}
}

func (d *BoolDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *BoolDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *BoolDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *BoolDescriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if id != nil {
		idLen := LengthId(*id)
		if d.isPrt {
			if p == nil {
				return buf
			} else if *(*bool)(p) {
				buf = WriterType(buf, TBool, idLen, 2)
			} else {
				buf = WriterType(buf, TBool, idLen, 1)
			}
		} else {
			if p == nil {
				return buf
			} else if *(*bool)(p) {
				buf = WriterType(buf, TBool, idLen, 2)
			} else {
				return buf
			}
		}
		return WriterId(buf, *id)
	} else {
		if d.isPrt {
			if p == nil {
				buf = WriterType(buf, TBool, 0, 1)
			} else if *(*bool)(p) {
				buf = WriterType(buf, TBool, 1, 2)
			} else {
				buf = WriterType(buf, TBool, 1, 1)
			}
		} else {
			if p == nil {
			} else if *(*bool)(p) {
				buf = WriterType(buf, TBool, 1, 2)
			} else {
				buf = WriterType(buf, TBool, 1, 1)
			}
		}
		return buf
	}
}

func (d *BoolDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TBool {
		return nil, errors.New("invalid bool type")
	}
	val := 2 == valueLen

	if p != nil {
		if d.isPrt {
			*(**bool)(p) = &val
		} else {
			*(*bool)(p) = val
		}
	}
	return buf, nil
}

func NewTimeDescriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &TimeDescriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type TimeDescriptor struct {
	offset uintptr
	isPrt  bool
	tags   map[string]bool
}

func (d *TimeDescriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return (*(*time.Time)(ptr)).IsZero()
}

func (d *TimeDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *TimeDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if len(tag) > 0 && !d.tags[tag] {
		return nil
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *TimeDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *TimeDescriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || (*(*time.Time)(p)).IsZero() {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TInt, 2, 1)
		} else {
			return WriterType(buf, TInt, 0, 1)
		}
	}

	val := (*(*time.Time)(p)).UnixMicro()
	if id == nil {
		buf = WriterType(buf, TInt, 1, LengthUint(uint64(val)))
	} else {
		buf = WriterType(buf, TInt, LengthId(*id), LengthUint(uint64(val)))
		buf = WriterId(buf, *id)
	}
	return EncodeUint64(buf, uint64(val))
}

func (d *TimeDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid time type")
	}
	var val uint64
	if valRead {
		val, buf = DecodeUint64(buf, valueLen)
	}
	if p != nil {
		if d.isPrt {
			temp := time.UnixMicro(int64(val))
			*(**time.Time)(p) = &temp
		} else {
			*(*time.Time)(p) = time.UnixMicro(int64(val))
		}
	}
	return buf, nil
}

func NewDecimalDescriptor(offset uintptr, isPrt bool, tags ...string) Descriptor {
	return &DecimalDescriptor{
		offset: offset,
		isPrt:  isPrt,
		tags:   listToSet(tags),
	}
}

type DecimalDescriptor struct {
	offset uintptr
	tags   map[string]bool
	isPrt  bool
}

func (d *DecimalDescriptor) IsEmpty(p unsafe.Pointer, tag string) bool {
	if len(tag) > 0 && !d.tags[tag] {
		return true
	}

	ptr := unsafe.Add(p, d.offset)
	if d.isPrt {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	return (*(*decimal.Decimal)(ptr)).IsZero()
}

func (d *DecimalDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *DecimalDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	return ptr
}

func (d *DecimalDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *DecimalDescriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16, tag string) []byte {
	if p == nil || (*(*decimal.Decimal)(p)).IsZero() {
		if id != nil {
			return buf
		} else if p != nil {
			return WriterType(buf, TBytes, 2, 1)
		} else {
			return WriterType(buf, TBytes, 0, 1)
		}
	}

	val := (*(*decimal.Decimal)(p)).String()
	size := uint64(len(val))
	if id == nil {
		buf = WriterType(buf, TBytes, 1, LengthUint(size))
	} else {
		buf = WriterType(buf, TBytes, LengthId(*id), LengthUint(size))
		buf = WriterId(buf, *id)
	}
	buf = EncodeUint64(buf, size)
	return append(buf, val...)
}

func (d *DecimalDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8, tag string) ([]byte, error) {
	if typ != TBytes {
		return nil, errors.New("invalid decimal type")
	}

	var err error
	var size uint64
	var val decimal.Decimal
	if valRead {
		size, buf = DecodeUint64(buf, valueLen)
		val, err = decimal.NewFromString(string(buf[:size]))
		if err != nil {
			return nil, err
		}
		buf = buf[size:]
	}
	if p != nil {
		if d.isPrt {
			*(**decimal.Decimal)(p) = &val
		} else {
			*(*decimal.Decimal)(p) = val
		}
	}
	return buf, nil
}
