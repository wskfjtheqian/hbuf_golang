package hbuf

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/shopspring/decimal"
)

// ParseTypeDescriptor 从 struct tag（hbuf:"id"）反射构建 DataDescriptor，替代手写描述符表。
//
// 约定：
//   - 普通字段 tag：`hbuf:"1"`；扩展字段 tag：`hbuf:"1,ext"`
//   - 指针字段（*T）即 isPtr；列表/映射元素同理
//   - 自定义 data 类型按实现 Data 接口识别，递归类型经 RefDescriptor 延迟构建
//
// 构建结果按类型缓存（一次反射开销，编解码路径与手写描述符一致走 unsafe）。
func ParseTypeDescriptor(t reflect.Type) Descriptor {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		panic("hbuf: ParseTypeDescriptor only supports struct, got " + t.String())
	}
	if d, ok := tagDescCache.Load(t); ok {
		return d.(Descriptor)
	}
	d := buildDataDescriptor(t)
	tagDescCache.Store(t, d)
	return d
}

var tagDescCache sync.Map // map[reflect.Type]Descriptor

// buildDataDescriptor 构建过程中不写缓存，避免递归构建半成品入缓存。
func buildDataDescriptor(t reflect.Type) Descriptor {
	fields := map[uint16]Descriptor{}
	extends := map[uint16]Descriptor{}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("hbuf")
		if tag == "" || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		id, err := strconv.ParseUint(parts[0], 10, 16)
		if err != nil {
			panic("hbuf: invalid hbuf tag on " + t.String() + "." + f.Name + ": " + tag)
		}
		d := descOfField(f.Type, f.Offset)
		if len(parts) > 1 && parts[1] == "ext" {
			extends[uint16(id)] = d
		} else {
			fields[uint16(id)] = d
		}
	}

	return NewDataDescriptor(0, false, reflect.PtrTo(t), extends, fields)
}

// descOfField 按字段类型构建 Descriptor。offset 为字段在宿主结构中的偏移。
func descOfField(t reflect.Type, offset uintptr) Descriptor {
	if t.Kind() == reflect.Ptr {
		return descOfPtr(t.Elem(), offset)
	}
	// []byte 精确匹配（命名类型如 type Uint8List []uint8 仍按列表处理）
	if t == reflect.TypeOf([]byte{}) {
		return NewBytesDescriptor(offset, false)
	}
	if t.Kind() == reflect.Slice {
		elem := descOfElem(t.Elem())
		return newReflectListDescriptor(t, elem, offset, false)
	}
	if t.Kind() == reflect.Map {
		return newReflectMapDescriptor(t, offset, false)
	}
	return descOfElem(t).(withOffset).WithOffset(offset)
}

// withOffset 让元素描述符绑定宿主偏移（标量/字符串/时间/decimal/data 引用均支持）。
type withOffset interface {
	WithOffset(offset uintptr) Descriptor
}

// descOfElem 构建元素描述符（offset=0，由调用方通过 SetValue 传入元素地址）。
func descOfElem(t reflect.Type) Descriptor {
	switch t {
	case reflect.TypeOf(int8(0)):
		return NewInt8Descriptor(0, false)
	case reflect.TypeOf(int16(0)):
		return NewInt16Descriptor(0, false)
	case reflect.TypeOf(int32(0)):
		return NewInt32Descriptor(0, false)
	case reflect.TypeOf(int64(0)):
		return NewInt64Descriptor(0, false)
	case reflect.TypeOf(uint8(0)):
		return NewUint8Descriptor(0, false)
	case reflect.TypeOf(uint16(0)):
		return NewUint16Descriptor(0, false)
	case reflect.TypeOf(uint32(0)):
		return NewUint32Descriptor(0, false)
	case reflect.TypeOf(uint64(0)):
		return NewUint64Descriptor(0, false)
	case reflect.TypeOf(false):
		return NewBoolDescriptor(0, false)
	case reflect.TypeOf(""):
		return NewStringDescriptor(0, false)
	case reflect.TypeOf(float32(0)):
		return NewFloatDescriptor(0, false)
	case reflect.TypeOf(float64(0)):
		return NewDoubleDescriptor(0, false)
	case reflect.TypeOf(time.Time{}):
		return NewTimeDescriptor(0, false)
	case reflect.TypeOf(decimal.Decimal{}):
		return NewDecimalDescriptor(0, false)
	}
	if t.Kind() == reflect.Struct {
		if d, ok := reflect.New(t).Interface().(Data); ok {
			return NewRefDescriptor(d, 0, false)
		}
		// 非 Data 结构体：panic 在构建期暴露，优于运行时静默跳过
		panic("hbuf: unsupported struct type " + t.String())
	}
	if t.Kind() == reflect.Ptr {
		return descOfPtr(t.Elem(), 0)
	}
	if t == reflect.TypeOf([]byte{}) {
		return NewBytesDescriptor(0, false)
	}
	if t.Kind() == reflect.Slice {
		return newReflectListDescriptor(t, descOfElem(t.Elem()), 0, false)
	}
	if t.Kind() == reflect.Map {
		return newReflectMapDescriptor(t, 0, false)
	}
	panic("hbuf: unsupported type " + t.String())
}

// descOfPtr 指针字段：isPtr=true 的目标描述符。
func descOfPtr(t reflect.Type, offset uintptr) Descriptor {
	switch t {
	case reflect.TypeOf(int8(0)):
		return NewInt8Descriptor(offset, true)
	case reflect.TypeOf(int16(0)):
		return NewInt16Descriptor(offset, true)
	case reflect.TypeOf(int32(0)):
		return NewInt32Descriptor(offset, true)
	case reflect.TypeOf(int64(0)):
		return NewInt64Descriptor(offset, true)
	case reflect.TypeOf(uint8(0)):
		return NewUint8Descriptor(offset, true)
	case reflect.TypeOf(uint16(0)):
		return NewUint16Descriptor(offset, true)
	case reflect.TypeOf(uint32(0)):
		return NewUint32Descriptor(offset, true)
	case reflect.TypeOf(uint64(0)):
		return NewUint64Descriptor(offset, true)
	case reflect.TypeOf(false):
		return NewBoolDescriptor(offset, true)
	case reflect.TypeOf(""):
		return NewStringDescriptor(offset, true)
	case reflect.TypeOf(float32(0)):
		return NewFloatDescriptor(offset, true)
	case reflect.TypeOf(float64(0)):
		return NewDoubleDescriptor(offset, true)
	case reflect.TypeOf(time.Time{}):
		return NewTimeDescriptor(offset, true)
	case reflect.TypeOf(decimal.Decimal{}):
		return NewDecimalDescriptor(offset, true)
	case reflect.TypeOf([]byte{}):
		return NewBytesDescriptor(offset, true)
	}
	if t.Kind() == reflect.Struct {
		if d, ok := reflect.New(t).Interface().(Data); ok {
			return NewRefDescriptor(d, offset, true)
		}
		panic("hbuf: unsupported struct type " + t.String())
	}
	if t.Kind() == reflect.Slice {
		elemTyp := t.Elem()
		var elemDesc Descriptor
		if elemTyp.Kind() == reflect.Ptr {
			elemDesc = descOfPtr(elemTyp.Elem(), 0)
		} else {
			elemDesc = descOfElem(elemTyp)
		}
		return newReflectListDescriptor(reflect.SliceOf(elemTyp), elemDesc, offset, true)
	}
	if t.Kind() == reflect.Map {
		return newReflectMapDescriptor(t, offset, true)
	}
	panic("hbuf: unsupported pointer type " + t.String())
}

// ===================== 反射列表描述符（行为与泛型 ListDescriptor 一致） =====================

// sliceHeader 与 Go 运行时 slice 布局一致（data/len/cap 三字），
// 用于直接读写字段内存中的切片头，避免 reflect.SliceHeader 的 vet 误用告警。
type sliceHeader struct {
	Data unsafe.Pointer
	Len  int
	Cap  int
}

type reflectListDescriptor struct {
	offset   uintptr
	elem     Descriptor
	elemSize uintptr
	sliceTyp reflect.Type
	isPtr    bool
}

func newReflectListDescriptor(sliceTyp reflect.Type, elem Descriptor, offset uintptr, isPtr bool) Descriptor {
	return &reflectListDescriptor{
		offset:   offset,
		elem:     elem,
		elemSize: sliceTyp.Elem().Size(),
		sliceTyp: sliceTyp,
		isPtr:    isPtr,
	}
}

func (d *reflectListDescriptor) IsEmpty(p unsafe.Pointer) bool {
	ptr := unsafe.Add(p, d.offset)
	if d.isPtr {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	if ptr == nil {
		return true
	}
	hdr := *(*sliceHeader)(ptr)
	if hdr.Len == 0 {
		return !d.isPtr
	}
	return false
}

func (d *reflectListDescriptor) GetValue(p unsafe.Pointer) unsafe.Pointer {
	ptr := unsafe.Add(p, d.offset)
	if d.isPtr {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *reflectListDescriptor) SetValue(p unsafe.Pointer) unsafe.Pointer {
	if p != nil {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *reflectListDescriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16) []byte {
	var hdr sliceHeader
	if p == nil {
		if id != nil {
			return buf
		}
		if d.isPtr {
			return WriterType(buf, TList, 0, 1)
		}
		return WriterType(buf, TList, 0, 1)
	}
	hdr = *(*sliceHeader)(p)
	if id != nil {
		if d.isPtr && p == nil {
			return buf
		}
		if !d.isPtr && hdr.Len == 0 {
			return buf
		}
		buf = WriterType(buf, TList, LengthId(*id), LengthUint(uint64(hdr.Len)))
		buf = WriterId(buf, *id)
	} else {
		if hdr.Len == 0 {
			return WriterType(buf, TList, 0, 1)
		}
		buf = WriterType(buf, TList, 1, LengthUint(uint64(hdr.Len)))
	}
	buf = EncodeUint64(buf, uint64(hdr.Len))
	for i := 0; i < hdr.Len; i++ {
		elemPtr := unsafe.Add(hdr.Data, uintptr(i)*d.elemSize)
		buf = d.elem.Encode(buf, d.elem.GetValue(elemPtr), nil)
	}
	return buf
}

func (d *reflectListDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8) ([]byte, error) {
	if typ != TList {
		return nil, errors.New("invalid list type")
	}
	var count uint64
	var err error
	var idLen uint8

	if valRead {
		count, buf, err = DecodeUint64(buf, valueLen)
		if err != nil {
			return buf, err
		}
	}
	if count == 0 {
		return buf, nil
	}
	if p != nil {
		list := reflect.MakeSlice(d.sliceTyp, int(count), int(count))
		for i := uint64(0); i < count; i++ {
			typ, idLen, valueLen, buf, err = DecodeType(buf)
			if err != nil {
				return buf, err
			}
			if idLen > 0 {
				elemPtr := list.Index(int(i)).Addr().UnsafePointer()
				buf, err = d.elem.Decode(buf, d.elem.SetValue(elemPtr), typ, idLen == 1, valueLen)
				if err != nil {
					return buf, err
				}
			}
		}
		hdr := sliceHeader{Data: list.Index(0).Addr().UnsafePointer(), Len: int(count), Cap: int(count)}
		if d.isPtr {
			*(*unsafe.Pointer)(p) = unsafe.Pointer(&hdr)
		} else {
			*(*sliceHeader)(p) = hdr
		}
	} else {
		for i := uint64(0); i < count; i++ {
			typ, idLen, valueLen, buf, err = DecodeType(buf)
			if err != nil {
				return buf, err
			}
			if idLen > 0 {
				buf, err = d.elem.Decode(buf, nil, typ, idLen == 1, valueLen)
				if err != nil {
					return buf, err
				}
			}
		}
	}
	return buf, nil
}

// ===================== 反射映射描述符（行为与泛型 MapDescriptor 一致） =====================

type reflectMapDescriptor struct {
	offset  uintptr
	mapTyp  reflect.Type
	keyDesc Descriptor
	valDesc Descriptor
	isPtr   bool
}

func newReflectMapDescriptor(mapTyp reflect.Type, offset uintptr, isPtr bool) Descriptor {
	return &reflectMapDescriptor{
		offset:  offset,
		mapTyp:  mapTyp,
		keyDesc: descOfPtrOrElem(mapTyp.Key()),
		valDesc: descOfPtrOrElem(mapTyp.Elem()),
		isPtr:   isPtr,
	}
}

// descOfPtrOrElem 映射键/值：指针类型转 isPtr 描述符，否则元素描述符。
func descOfPtrOrElem(t reflect.Type) Descriptor {
	if t.Kind() == reflect.Ptr {
		return descOfPtr(t.Elem(), 0)
	}
	return descOfElem(t)
}

func (d *reflectMapDescriptor) IsEmpty(p unsafe.Pointer) bool {
	ptr := unsafe.Add(p, d.offset)
	if d.isPtr {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return true
		}
	}
	if ptr == nil {
		return true
	}
	m := reflect.NewAt(d.mapTyp, ptr).Elem()
	if m.Len() == 0 {
		return !d.isPtr
	}
	return false
}

func (d *reflectMapDescriptor) GetValue(p unsafe.Pointer) unsafe.Pointer {
	ptr := unsafe.Add(p, d.offset)
	if d.isPtr {
		ptr = *(*unsafe.Pointer)(ptr)
		if ptr == nil {
			return nil
		}
	}
	return ptr
}

func (d *reflectMapDescriptor) SetValue(p unsafe.Pointer) unsafe.Pointer {
	if p != nil {
		return unsafe.Add(p, d.offset)
	}
	return nil
}

func (d *reflectMapDescriptor) Encode(buf []byte, p unsafe.Pointer, id *uint16) []byte {
	if p == nil {
		if id != nil {
			return buf
		}
		return WriterType(buf, TMap, 0, 1)
	}
	m := reflect.NewAt(d.mapTyp, p).Elem()
	if id != nil {
		if d.isPtr && p == nil {
			return buf
		}
		if !d.isPtr && m.Len() == 0 {
			return buf
		}
		buf = WriterType(buf, TMap, LengthId(*id), LengthUint(uint64(m.Len())))
		buf = WriterId(buf, *id)
	} else {
		if m.Len() == 0 {
			return WriterType(buf, TMap, 0, 1)
		}
		buf = WriterType(buf, TMap, 1, LengthUint(uint64(m.Len())))
	}
	buf = EncodeUint64(buf, uint64(m.Len()))
	iter := m.MapRange()
	for iter.Next() {
		// 装箱取出地址（每元素 2 次分配，仅反射构建路径）
		kt := reflect.New(iter.Key().Type())
		kt.Elem().Set(iter.Key())
		vt := reflect.New(iter.Value().Type())
		vt.Elem().Set(iter.Value())
		buf = d.keyDesc.Encode(buf, d.keyDesc.GetValue(kt.UnsafePointer()), nil)
		buf = d.valDesc.Encode(buf, d.valDesc.GetValue(vt.UnsafePointer()), nil)
	}
	return buf
}

func (d *reflectMapDescriptor) Decode(buf []byte, p unsafe.Pointer, typ Type, valRead bool, valueLen uint8) ([]byte, error) {
	if typ != TMap {
		return nil, errors.New("invalid map type")
	}
	var count uint64
	var err error
	var idLen uint8

	if valRead {
		count, buf, err = DecodeUint64(buf, valueLen)
		if err != nil {
			return buf, err
		}
	}
	if count == 0 {
		return buf, nil
	}
	m := reflect.MakeMapWithSize(d.mapTyp, int(count))
	for i := uint64(0); i < count; i++ {
		kt := reflect.New(d.mapTyp.Key())
		typ, idLen, valueLen, buf, err = DecodeType(buf)
		if err != nil {
			return buf, err
		}
		if idLen > 0 {
			buf, err = d.keyDesc.Decode(buf, d.keyDesc.SetValue(kt.UnsafePointer()), typ, idLen == 1, valueLen)
			if err != nil {
				return buf, err
			}
		}
		vt := reflect.New(d.mapTyp.Elem())
		typ, idLen, valueLen, buf, err = DecodeType(buf)
		if err != nil {
			return buf, err
		}
		if idLen > 0 {
			buf, err = d.valDesc.Decode(buf, d.valDesc.SetValue(vt.UnsafePointer()), typ, idLen == 1, valueLen)
			if err != nil {
				return buf, err
			}
		}
		m.SetMapIndex(kt.Elem(), vt.Elem())
	}
	if p != nil {
		if d.isPtr {
			mp := reflect.New(d.mapTyp)
			mp.Elem().Set(m)
			*(*unsafe.Pointer)(p) = mp.UnsafePointer()
		} else {
			reflect.NewAt(d.mapTyp, p).Elem().Set(m)
		}
	}
	return buf, nil
}
