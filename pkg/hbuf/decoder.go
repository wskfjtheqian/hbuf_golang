package hbuf

import (
	"errors"
	"io"
	"math"
	"reflect"
)

func Reader(r io.Reader) (typ Type, id uint16, valueLen uint8, err error) {
	var count int
	b := make([]byte, 1)
	count, err = r.Read(b)
	if err != nil {
		return
	}
	if count != 1 {
		err = errors.New("read fail, length error")
		return
	}
	typ = Type(b[0] >> 5 & 0b111)
	valueLen = (b[0] >> 2 & 0b111) + 1
	idLen := b[0] & 0b11

	if idLen > 0 {
		b = make([]byte, idLen)
		count, err = r.Read(b)
		if err != nil {
			return
		}

		if byte(count) != idLen {
			err = errors.New("read fail, length error")
		}
		if idLen == 1 {
			id = uint16(b[0])
		} else if idLen == 2 {
			id = uint16(b[0]) + uint16(b[1])<<8
		}
	}
	return
}

func DecodeInt64(reader io.Reader, typ Type, valueLen uint8) (v int64, err error) {
	b := make([]byte, valueLen)
	count, err := reader.Read(b)
	if err != nil {
		return
	}
	if count != int(valueLen) {
		return 0, errors.New("read fail, length error")
	}
	if valueLen == 1 {
		v = int64(b[0])
	} else if valueLen == 2 {
		v = int64(b[0]) + int64(b[1])<<8
	} else if valueLen == 3 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(b[2])<<16
	} else if valueLen == 4 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(b[2])<<16 + int64(b[3])<<24
	} else if valueLen == 5 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(b[2])<<16 + int64(b[3])<<24 + int64(b[4])<<32
	} else if valueLen == 6 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(b[2])<<16 + int64(b[3])<<24 + int64(b[4])<<32 + int64(b[5])<<40
	} else if valueLen == 7 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(b[2])<<16 + int64(b[3])<<24 + int64(b[4])<<32 + int64(b[5])<<40 + int64(b[6])<<48
	} else if valueLen == 8 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(b[2])<<16 + int64(b[3])<<24 + int64(b[4])<<32 + int64(b[5])<<40 + int64(b[6])<<48 + int64(b[7])<<56
	}
	return
}

func DecodeUint64(reader io.Reader, typ Type, valueLen uint8) (v uint64, err error) {
	b := make([]byte, valueLen)
	count, err := reader.Read(b)
	if err != nil {
		return
	}
	if count != int(valueLen) {
		return 0, errors.New("read fail, length error")
	}
	if valueLen == 1 {
		v = uint64(b[0])
	} else if valueLen == 2 {
		v = uint64(b[0]) + uint64(b[1])<<8
	} else if valueLen == 3 {
		v = uint64(b[0]) + uint64(b[1])<<8 + uint64(b[2])<<16
	} else if valueLen == 4 {
		v = uint64(b[0]) + uint64(b[1])<<8 + uint64(b[2])<<16 + uint64(b[3])<<24
	} else if valueLen == 5 {
		v = uint64(b[0]) + uint64(b[1])<<8 + uint64(b[2])<<16 + uint64(b[3])<<24 + uint64(b[4])<<32
	} else if valueLen == 6 {
		v = uint64(b[0]) + uint64(b[1])<<8 + uint64(b[2])<<16 + uint64(b[3])<<24 + uint64(b[4])<<32 + uint64(b[5])<<40
	} else if valueLen == 7 {
		v = uint64(b[0]) + uint64(b[1])<<8 + uint64(b[2])<<16 + uint64(b[3])<<24 + uint64(b[4])<<32 + uint64(b[5])<<40 + uint64(b[6])<<48
	} else if valueLen == 8 {
		v = uint64(b[0]) + uint64(b[1])<<8 + uint64(b[2])<<16 + uint64(b[3])<<24 + uint64(b[4])<<32 + uint64(b[5])<<40 + uint64(b[6])<<48 + uint64(b[7])<<56
	}
	return
}

func DecodeFloat(reader io.Reader, typ Type, valueLen uint8) (v float32, err error) {
	var value uint64
	value, err = DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return
	}

	v = math.Float32frombits(uint32(value))
	return
}

func DecodeDouble(reader io.Reader, typ Type, valueLen uint8) (v float64, err error) {
	var value uint64
	value, err = DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return
	}

	v = math.Float64frombits(value)
	return
}

func DecodeBytes(reader io.Reader, typ Type, valueLen uint8) (v []byte, err error) {
	var value uint64
	var count int
	value, err = DecodeUint64(reader, typ, valueLen)
	if err != nil {
		return
	}

	v = make([]byte, value)
	count, err = reader.Read(v)
	if err != nil {
		return
	}
	if count != int(value) {
		return nil, errors.New("read fail, length error")
	}
	return
}

func DecodeBool(reader io.Reader, typ Type, valueLen uint8) (v bool, err error) {
	return true, nil
}

type Decoder struct {
	io.Reader
	end uint64
}

func NewDecoder(reader io.Reader) *Decoder {
	return &Decoder{Reader: reader}
}

func (d *Decoder) Decode(v any) (err error) {
	var typ Type
	//var id uint16
	var valueLen uint8

	kind := reflect.TypeOf(v).Kind()
	if kind != reflect.Ptr {
		return errors.New("decode fail, not a pointer")
	}

	if _, ok := v.(Data); ok {
		typ, _, valueLen, err = Reader(d)

		return NewDataDescriptor(func(d any) Data {
			return d.(Data)
		}, func(d any, v Data) {
			d = v
		}).Decode(d.Reader, v, typ, valueLen)
	}
	return
	//
	//t := reflect.ValueOf(v).Elem().Interface()
	//kind = reflect.TypeOf(t).Kind()
	//
	//for {
	//	typ, _, valueLen, err = Reader(d)
	//	if err != nil {
	//		if err == io.EOF {
	//			return nil
	//		}
	//		return
	//	}
	//
	//	switch kind {
	//	case reflect.Int8:
	//		{
	//			var value int64
	//			value, err = DecodeInt64(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*int8)) = int8(value)
	//		}
	//	case reflect.Int16:
	//		{
	//			var value int64
	//			value, err = DecodeInt64(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*int16)) = int16(value)
	//		}
	//	case reflect.Int32:
	//		{
	//			var value int64
	//			value, err = DecodeInt64(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*int32)) = int32(value)
	//		}
	//	case reflect.Int64:
	//		{
	//			var value int64
	//			value, err = DecodeInt64(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*int64)) = value
	//		}
	//	case reflect.Uint8:
	//		{
	//			var value uint64
	//			value, err = DecodeUint64(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*uint8)) = uint8(value)
	//		}
	//	case reflect.Uint16:
	//		{
	//			var value uint64
	//			value, err = DecodeUint64(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*uint16)) = uint16(value)
	//		}
	//	case reflect.Uint32:
	//		{
	//			var value uint64
	//			value, err = DecodeUint64(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*uint32)) = uint32(value)
	//		}
	//	case reflect.Uint64:
	//		{
	//			var value uint64
	//			value, err = DecodeUint64(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*uint64)) = value
	//		}
	//	case reflect.Float32:
	//		{
	//			var value float32
	//			value, err = DecodeFloat(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*float32)) = value
	//		}
	//	case reflect.Float64:
	//		{
	//			var value float64
	//			value, err = DecodeDouble(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*float64)) = value
	//		}
	//	case reflect.Bool:
	//		{
	//			var value bool
	//			value, err = DecodeBool(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*bool)) = value
	//		}
	//	case reflect.String:
	//		{
	//			var value []byte
	//			value, err = DecodeBytes(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*string)) = string(value)
	//		}
	//	case reflect.Slice:
	//		{
	//			var value []byte
	//			value, err = DecodeBytes(d, typ, valueLen)
	//			if err != nil {
	//				return
	//			}
	//			*(v.(*[]byte)) = value
	//		}
	//	default:
	//		return errors.New("decode fail, unsupported type")
	//	}
	//
	//}
}
