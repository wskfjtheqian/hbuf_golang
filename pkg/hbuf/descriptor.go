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

func (d *DataDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
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
		ptr := p
		if d.isPtr {
			data := reflect.New(d.typ.Elem())
			ptr = data.UnsafePointer()
			*(**unsafe.Pointer)(p) = (*unsafe.Pointer)(ptr)
		}

		for i := uint64(0); i < count; i++ {
			typ, id, valueLen, buf = Reader(buf)

			if field, ok := d.fieldMap[id]; ok {
				buf, err = field.Decode(buf, field.SetValue(ptr, tag), typ, valueLen, tag)
				if err != nil {
					return nil, err
				}
			}
		}

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

func (d *ListDescriptor[T]) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
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
		*(*[]T)(p) = list
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
		*(*map[K]V)(p) = m
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

func (d *StringDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
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
			*((**string)(p)) = &val
		} else {
			*((*string)(p)) = string(buf[:size])
		}
	}
	return buf[size:], nil
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
	if len(*(*[]byte)(ptr)) == 0 {
		return nil
	}
	return ptr
}

func (d *BytesDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *BytesDescriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val []byte
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*[]byte)(p)
	}
	size := len(val)

	buf = WriterTypeId(buf, TBytes, id, LengthUint(uint64(size)))
	buf = WriterUint64(buf, uint64(size))
	return append(buf, val...)
}

func (d *BytesDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TBytes {
		return nil, errors.New("invalid bytes type")
	}

	size, buf := DecodeUint64(buf, valueLen)
	if p != nil {
		val := make([]byte, size)
		copy(val, buf[:size])
		if d.isPrt {
			*(**[]byte)(p) = &val
		} else {
			*(*[]byte)(p) = val
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

func (d *Int64Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
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
	if typ != TInt {
		return nil, errors.New("invalid int64 type")
	}

	val, buf := DecodeInt64(buf, valueLen)
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

func (d *Int32Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Int32Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	if *(*int32)(ptr) == 0 {
		return nil
	}
	return ptr
}

func (d *Int32Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Int32Descriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val int32
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*int32)(p)
	}

	buf = WriterTypeId(buf, TInt, id, LengthInt(int64(val)))
	return WriterInt64(buf, int64(val))
}

func (d *Int32Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid int32 type")
	}

	val, buf := DecodeInt64(buf, valueLen)
	if p != nil {
		if d.isPrt {
			val32 := int32(val)
			*(**int32)(p) = &val32
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

func (d *Int16Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Int16Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	if *(*int16)(ptr) == 0 {
		return nil
	}
	return ptr
}

func (d *Int16Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Int16Descriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val int16
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*int16)(p)
	}

	buf = WriterTypeId(buf, TInt, id, LengthInt(int64(val)))
	return WriterInt64(buf, int64(val))
}

func (d *Int16Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid int16 type")
	}

	val, buf := DecodeInt64(buf, valueLen)
	if p != nil {
		if d.isPrt {
			val16 := int16(val)
			*(**int16)(p) = &val16
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

func (d *Int8Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Int8Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	if *(*int8)(ptr) == 0 {
		return nil
	}
	return ptr
}

func (d *Int8Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Int8Descriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val int8
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*int8)(p)
	}

	buf = WriterTypeId(buf, TInt, id, LengthInt(int64(val)))
	return WriterInt64(buf, int64(val))
}

func (d *Int8Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TInt {
		return nil, errors.New("invalid int8 type")
	}

	val, buf := DecodeInt64(buf, valueLen)
	if p != nil {
		if d.isPrt {
			val8 := int8(val)
			*(**int8)(p) = &val8
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

func (d *Uint64Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Uint64Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	if *(*uint64)(ptr) == 0 {
		return nil
	}
	return ptr
}

func (d *Uint64Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Uint64Descriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val uint64
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*uint64)(p)
	}

	buf = WriterTypeId(buf, TUint, id, LengthUint(val))
	return WriterUint64(buf, val)
}

func (d *Uint64Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TUint {
		return nil, errors.New("invalid uint64 type")
	}

	val, buf := DecodeUint64(buf, valueLen)
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

func (d *Uint32Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Uint32Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	if *(*uint32)(ptr) == 0 {
		return nil
	}
	return ptr
}

func (d *Uint32Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Uint32Descriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val uint32
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*uint32)(p)
	}

	buf = WriterTypeId(buf, TUint, id, LengthUint(uint64(val)))
	return WriterUint64(buf, uint64(val))
}

func (d *Uint32Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TUint {
		return nil, errors.New("invalid uint32 type")
	}

	val, buf := DecodeUint64(buf, valueLen)
	if p != nil {
		if d.isPrt {
			val32 := uint32(val)
			*(**uint32)(p) = &val32
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

func (d *Uint16Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Uint16Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	if *(*uint16)(ptr) == 0 {
		return nil
	}
	return ptr
}

func (d *Uint16Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Uint16Descriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val uint16
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*uint16)(p)
	}

	buf = WriterTypeId(buf, TUint, id, LengthUint(uint64(val)))
	return WriterUint64(buf, uint64(val))
}

func (d *Uint16Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TUint {
		return nil, errors.New("invalid uint16 type")
	}
	val, buf := DecodeUint64(buf, valueLen)
	if p != nil {
		if d.isPrt {
			val16 := uint16(val)
			*(**uint16)(p) = &val16
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

func (d *Uint8Descriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *Uint8Descriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	if *(*uint8)(ptr) == 0 {
		return nil
	}
	return ptr
}

func (d *Uint8Descriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *Uint8Descriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val uint8
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*uint8)(p)
	}

	buf = WriterTypeId(buf, TUint, id, LengthUint(uint64(val)))
	return WriterUint64(buf, uint64(val))
}

func (d *Uint8Descriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TUint {
		return nil, errors.New("invalid uint8 type")
	}

	val, buf := DecodeUint64(buf, valueLen)
	if p != nil {
		if d.isPrt {
			val8 := uint8(val)
			*(**uint8)(p) = &val8
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

func (d *DoubleDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *DoubleDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *DoubleDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	if *(*float64)(ptr) == 0 {
		return nil
	}
	return ptr
}

func (d *DoubleDescriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val uint64
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*uint64)(p)
	}

	buf = WriterTypeId(buf, TFloat, id, LengthUint(val))
	return WriterUint64(buf, val)
}

func (d *DoubleDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TFloat {
		return nil, errors.New("invalid float64 type")
	}

	val, buf := DecodeUint64(buf, valueLen)
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

func (d *FloatDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *FloatDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	if *(*float32)(ptr) == 0 {
		return nil
	}
	return ptr
}

func (d *FloatDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *FloatDescriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val uint32
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*uint32)(p)
	}

	buf = WriterTypeId(buf, TFloat, id, LengthUint(uint64(val)))
	return WriterUint64(buf, uint64(val))
}

func (d *FloatDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TFloat {
		return nil, errors.New("invalid float32 type")
	}

	val, buf := DecodeUint64(buf, valueLen)
	if p != nil {
		if d.isPrt {
			value := uint32(val)
			*(**uint32)(p) = &value
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

func (d *BoolDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *BoolDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	if *(*bool)(ptr) == false {
		return nil
	}
	return ptr
}

func (d *BoolDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *BoolDescriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val bool
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*bool)(p)
	}
	if val {
		return WriterTypeId(buf, TBool, id, 2)
	}
	return WriterTypeId(buf, TBool, id, 1)
}

func (d *BoolDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TBool {
		return nil, errors.New("invalid bool type")
	}

	val := valueLen == 2
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

func (d *TimeDescriptor) SetTag(tags map[string]bool) {
	d.tags = tags
}

func (d *TimeDescriptor) GetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
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
	if time.Time(*(*Time)(ptr)).IsZero() {
		return nil
	}
	return ptr
}

func (d *TimeDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *TimeDescriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val time.Time
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*time.Time)(p)
	}

	value := val.UnixMicro()
	buf = WriterTypeId(buf, TUint, id, LengthUint(uint64(value)))
	buf = WriterUint64(buf, uint64(value))
	return buf
}

func (d *TimeDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TUint {
		return nil, errors.New("invalid time type")
	}

	val, buf := DecodeUint64(buf, valueLen)
	if p != nil {
		t := time.UnixMicro(int64(val))
		if d.isPrt {
			*(**time.Time)(p) = &t
		} else {
			*(*time.Time)(p) = t
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
	isPrt  bool
	tags   map[string]bool
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
	if (*(*decimal.Decimal)(ptr)).IsZero() {
		return nil
	}
	return ptr
}

func (d *DecimalDescriptor) SetValue(p unsafe.Pointer, tag string) unsafe.Pointer {
	if p != nil && (len(tag) == 0 || d.tags[tag]) {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *DecimalDescriptor) Encode(buf []byte, p unsafe.Pointer, id uint16, null bool, tag string) []byte {
	var val decimal.Decimal
	if p == nil {
		if !null {
			return buf
		}
	} else {
		val = *(*decimal.Decimal)(p)
	}

	value := val.String()
	size := len(value)

	buf = WriterTypeId(buf, TBytes, id, LengthUint(uint64(size)))
	buf = WriterUint64(buf, uint64(size))
	return append(buf, value...)
}

func (d *DecimalDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valueLen uint8, tag string) ([]byte, error) {
	if typ != TBytes {
		return nil, errors.New("invalid decimal type")
	}

	size, buf := DecodeUint64(buf, valueLen)
	if p != nil {
		value, err := decimal.NewFromString(string(buf[:size]))
		if err != nil {
			return nil, err
		}
		if d.isPrt {
			*(**decimal.Decimal)(p) = &value
		} else {
			*(*decimal.Decimal)(p) = value
		}
	}
	return buf[size:], nil
}
