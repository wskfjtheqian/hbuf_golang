package hbuf

import (
	"errors"
	"io"
	"math"
	"reflect"
	"unsafe"
)

func Writer(w io.Writer, typ Type, id uint16, valueLen uint8) (err error) {
	var b []byte
	var idLen uint8
	if id == 0 {
		b = make([]byte, 1)
	} else if id <= 0xFF {
		idLen = 1
		b = make([]byte, 2)
		b[1] = byte(id)
	} else {
		idLen = 2
		b = make([]byte, 3)
		b[1] = byte(id)
		b[2] = byte(id >> 8)
	}

	b[0] = byte(typ&0b111) << 5
	b[0] |= byte(int(valueLen-1)&0b111) << 2
	b[0] |= byte(idLen) & 0b11

	_, err = w.Write(b)
	return
}

func LengthInt(v int64) uint8 {
	if v >= -0x80 && v < 0x80 {
		return 1
	} else if v >= -0x8000 && v < 0x8000 {
		return 2
	} else if v >= -0x800000 && v < 0x800000 {
		return 3
	} else if v >= -0x80000000 && v < 0x80000000 {
		return 4
	} else if v >= -0x8000000000 && v < 0x8000000000 {
		return 5
	} else if v >= -0x800000000000 && v < 0x800000000000 {
		return 6
	} else if v >= -0x80000000000000 && v < 0x80000000000000 {
		return 7
	} else {
		return 8
	}
}

func LengthUint(h uint64) uint8 {
	if h <= 0xFF {
		return 1
	} else if h <= 0xFFFF {
		return 2
	} else if h <= 0xFFFFFF {
		return 3
	} else if h <= 0xFFFFFFFF {
		return 4
	} else if h <= 0xFFFFFFFFFF {
		return 5
	} else if h <= 0xFFFFFFFFFFFF {
		return 6
	} else if h <= 0xFFFFFFFFFFFFFF {
		return 7
	} else {
		return 8
	}
}

func WriterInt64(writer io.Writer, v int64) (err error) {
	if v >= -0x80 && v < 0x80 {
		_, err = writer.Write([]byte{byte(v)})
	} else if v >= -0x8000 && v < 0x8000 {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8)})
	} else if v >= -0x800000 && v < 0x800000 {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16)})
	} else if v >= -0x80000000 && v < 0x80000000 {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
	} else if v >= -0x8000000000 && v < 0x8000000000 {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24), byte(v >> 32)})
	} else if v >= -0x800000000000 && v < 0x800000000000 {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24), byte(v >> 32), byte(v >> 40)})
	} else if v >= -0x80000000000000 && v < 0x80000000000000 {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24), byte(v >> 32), byte(v >> 40), byte(v >> 48)})
	} else {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24), byte(v >> 32), byte(v >> 40), byte(v >> 48), byte(v >> 56)})
	}
	return
}

func WriterUint64(writer io.Writer, v uint64) (err error) {
	if v <= 0xFF {
		_, err = writer.Write([]byte{byte(v)})
	} else if v <= 0xFFFF {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8)})
	} else if v <= 0xFFFFFF {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16)})
	} else if v <= 0xFFFFFFFF {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
	} else if v <= 0xFFFFFFFFFF {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24), byte(v >> 32)})
	} else if v <= 0xFFFFFFFFFFFF {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24), byte(v >> 32), byte(v >> 40)})
	} else if v <= 0xFFFFFFFFFFFFFF {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24), byte(v >> 32), byte(v >> 40), byte(v >> 48)})
	} else {
		_, err = writer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24), byte(v >> 32), byte(v >> 40), byte(v >> 48), byte(v >> 56)})
	}
	return
}

func EncodeInt64(writer io.Writer, id uint16, v int64) (err error) {
	if v == 0 {
		return
	}
	err = Writer(writer, TInt, id, LengthInt(v))
	if err != nil {
		return
	}

	return WriterInt64(writer, v)
}

func EncodeUint64(writer io.Writer, id uint16, v uint64) (err error) {
	if v == 0 {
		return
	}
	err = Writer(writer, TUint, id, LengthUint(v))
	if err != nil {
		return
	}

	return WriterUint64(writer, v)
}

func EncodeFloat(writer io.Writer, id uint16, v float32) (err error) {
	if v == 0 {
		return
	}

	value := math.Float32bits(v)
	err = Writer(writer, TFloat, id, LengthUint(uint64(value)))
	if err != nil {
		return
	}

	return WriterUint64(writer, uint64(value))
}

func EncodeDouble(writer io.Writer, id uint16, v float64) (err error) {
	if v == 0 {
		return
	}

	value := math.Float64bits(v)
	err = Writer(writer, TFloat, id, LengthUint(value))
	if err != nil {
		return
	}

	return WriterUint64(writer, value)
}

func EncodeBytes(writer io.Writer, id uint16, s []byte) (err error) {
	length := len(s)
	if length == 0 {
		return
	}

	err = Writer(writer, TBytes, id, LengthUint(uint64(length)))
	if err != nil {
		return
	}

	err = WriterUint64(writer, uint64(length))
	if err != nil {
		return
	}

	_, err = writer.Write(s)
	return err
}

func EncodeBool(writer io.Writer, id uint16, v bool) (err error) {
	if v {
		return
	}
	err = Writer(writer, TBool, id, 1)
	if err != nil {
		return
	}
	return
}

type Encoder struct {
	w io.Writer
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

func (e *Encoder) Encode(v any) (err error) {
	if v == nil {
		return nil
	}

	if val, ok := v.(Data); ok {
		return NewDataDescriptor(func(v unsafe.Pointer) unsafe.Pointer {
			return v
		}, nil, val.Descriptor()).Encode(e.w, unsafe.Pointer(reflect.ValueOf(v).Pointer()), 0)
	}

	kind := reflect.TypeOf(v).Kind()
	if kind == reflect.Ptr {
		return e.Encode(reflect.Indirect(reflect.ValueOf(v)).Interface())
	}

	switch kind {
	case reflect.Int8:
		return EncodeInt64(e.w, 0, int64(v.(int8)))
	case reflect.Int16:
		return EncodeInt64(e.w, 0, int64(v.(int16)))
	case reflect.Int32:
		return EncodeInt64(e.w, 0, int64(v.(int32)))
	case reflect.Int64:
		return EncodeInt64(e.w, 0, v.(int64))
	case reflect.Uint8:
		return EncodeUint64(e.w, 0, uint64(v.(uint8)))
	case reflect.Uint16:
		return EncodeUint64(e.w, 0, uint64(v.(uint16)))
	case reflect.Uint32:
		return EncodeUint64(e.w, 0, uint64(v.(uint32)))
	case reflect.Uint64:
		return EncodeUint64(e.w, 0, v.(uint64))
	case reflect.Float32:
		return EncodeFloat(e.w, 0, v.(float32))
	case reflect.Float64:
		return EncodeDouble(e.w, 0, v.(float64))
	case reflect.Bool:
		return EncodeBool(e.w, 0, v.(bool))
	case reflect.String:
		return EncodeBytes(e.w, 0, []byte(v.(string)))
	case reflect.Slice:
		if reflect.TypeOf(v).Elem().Kind() == reflect.Uint8 {
			return EncodeBytes(e.w, 0, v.([]byte))
		}
	default:
	}
	return errors.New("encoder cannot encode type " + kind.String())
}
