package hbuf

import (
	"errors"
	"io"
	"math"
)

/*
Field 结构
------------------------------------------------
当 TInt TUint TFloat 的 TAG
类型 Type	| 值的长 uint64 	| ID长度 uint32
	111		| 111			| 111
TAG	| ID | VALUE
------------------------------------------------
当 TBytes  的 TAG
类型 Type	| 长度值的长 uint64 	| ID长度 uint32
	111		| 111				| 111
TAG	| ID | LEN | VALUE
------------------------------------------------
当 TList  的 TAG
类型 Type	| 数量值的长 uint64 	| ID长度 uint32
	111		| 111				| 111
TAG	| ID | COUNT | <VALUE | <VALUE> |...>
------------------------------------------------
当 TMap  的 TAG
类型 Type	| 是否有KEY	| 数量值的长 uint64 	| ID长度 uint32
	111		| 1			| 11				| 111
TAG	| ID | COUNT | <<KEY> | VALUE | <<KEY> | VALUE> | ...>
------------------------------------------------
当 TData  的 TAG
类型 Type	| 是否有Extend	| 数量值的长 uint64 	| ID长度 uint32
	111		| 1				| 11				| 111
TAG	| ID | COUNT | <EXTEND_COUNT | Extend| <> | ...> | <FIELD ...>

*/

type Type byte

const (
	TInt = iota + 0
	TUint
	TFloat
	TBytes
	TList
	TMap
	TData
)

func WriterField(w io.Writer, typ Type, id uint16, valueLen uint8) (err error) {
	var b []byte
	var length uint8
	if id <= 0xFF {
		length = 2
		b = make([]byte, 2)
		b[1] = byte(id)
	} else {
		length = 3
		b = make([]byte, 3)
		b[1] = byte(id)
		b[2] = byte(id >> 8)
	}

	b[0] = byte(typ&0xF) << 4
	b[0] |= (byte(length-2) & 0x1) << 3
	b[0] |= byte(valueLen-1) & 0x7

	_, err = w.Write(b)
	return
}

func ReaderField(r io.Reader) (typ Type, id uint16, valueLen uint8, err error) {
	var count int
	b := make([]byte, 1)
	count, err = r.Read(b)
	if err != nil {
		return
	}
	if count != 1 {
		err = errors.New("Read fail, length error")
	}
	valueLen = (b[0] & 0x7) + 1
	length := int((b[0] >> 3 & 0x1) + 1)
	typ = Type(b[0] >> 4 & 0xF)

	b = make([]byte, length)
	count, err = r.Read(b)
	if err != nil {
		return
	}
	if count != length {
		err = errors.New("Read fail, length error")
	}
	if length == 1 {
		id = uint16(b[0])
	} else if length == 2 {
		id = uint16(b[0]) | uint16(b[1])<<8
	}
	return
}

func EncoderInt64(v int64) (b []byte) {
	if v <= 0xFF {
		b = make([]byte, 1)
		b[0] = byte(v)
	} else if v <= 0xFFFF {
		b = make([]byte, 2)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
	} else if v <= 0xFFFFFF {
		b = make([]byte, 3)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
	} else if v <= 0xFFFFFFFF {
		b = make([]byte, 4)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
	} else if v <= 0xFFFFFFFFFF {
		b = make([]byte, 5)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
		b[4] = byte(v >> 32)
	} else if v <= 0xFFFFFFFFFFFF {
		b = make([]byte, 6)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
		b[4] = byte(v >> 32)
		b[5] = byte(v >> 40)
	} else if v <= 0xFFFFFFFFFFFFFF {
		b = make([]byte, 7)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
		b[4] = byte(v >> 32)
		b[5] = byte(v >> 40)
		b[6] = byte(v >> 48)
	} else {
		b = make([]byte, 8)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
		b[4] = byte(v >> 32)
		b[5] = byte(v >> 40)
		b[6] = byte(v >> 48)
		b[7] = byte(v >> 56)
	}
	return
}

func DecoderInt64(b []byte) int64 {
	length := len(b)
	if length == 1 {
		return int64(b[0])
	} else if length == 2 {
		return int64(b[0]) | int64(b[1])<<8
	} else if length == 3 {
		return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16
	} else if length == 4 {
		return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(b[3])<<24
	} else if length == 5 {
		return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(b[3])<<24 | int64(b[4])<<32
	} else if length == 6 {
		return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(b[3])<<24 | int64(b[4])<<32 | int64(b[5])<<40
	} else if length == 7 {
		return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(b[3])<<24 | int64(b[4])<<32 | int64(b[5])<<40 | int64(b[6])<<48
	} else if length == 7 {
		return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(b[3])<<24 | int64(b[4])<<32 | int64(b[5])<<40 | int64(b[6])<<48 | int64(b[7])<<56
	}
	return 0
}

func EncoderUint64(v uint64) (b []byte) {
	if v <= 0xFF {
		b = make([]byte, 1)
		b[0] = byte(v)
	} else if v <= 0xFFFF {
		b = make([]byte, 2)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
	} else if v <= 0xFFFFFF {
		b = make([]byte, 3)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
	} else if v <= 0xFFFFFFFF {
		b = make([]byte, 4)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
	} else if v <= 0xFFFFFFFFFF {
		b = make([]byte, 5)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
		b[4] = byte(v >> 32)
	} else if v <= 0xFFFFFFFFFFFF {
		b = make([]byte, 6)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
		b[4] = byte(v >> 32)
		b[5] = byte(v >> 40)
	} else if v <= 0xFFFFFFFFFFFFFF {
		b = make([]byte, 7)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
		b[4] = byte(v >> 32)
		b[5] = byte(v >> 40)
		b[6] = byte(v >> 48)
	} else {
		b = make([]byte, 8)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
		b[4] = byte(v >> 32)
		b[5] = byte(v >> 40)
		b[6] = byte(v >> 48)
		b[7] = byte(v >> 56)
	}
	return
}

func DecoderUint64(b []byte) uint64 {
	length := len(b)
	if length == 1 {
		return uint64(b[0])
	} else if length == 2 {
		return uint64(b[0]) | uint64(b[1])<<8
	} else if length == 3 {
		return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16
	} else if length == 4 {
		return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24
	} else if length == 5 {
		return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 | uint64(b[4])<<32
	} else if length == 6 {
		return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 | uint64(b[4])<<32 | uint64(b[5])<<40
	} else if length == 7 {
		return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 | uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48
	} else if length == 8 {
		return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 | uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
	}
	return 0
}

func EncoderFloat(v float32) (b []byte) {
	return EncoderUint64(uint64(math.Float32bits(v)))
}

func DecoderFloat(b []byte) float32 {
	return math.Float32frombits(uint32(DecoderUint64(b)))
}

func EncoderDouble(v float64) (b []byte) {
	return EncoderUint64(math.Float64bits(v))
}

func DecoderDouble(b []byte) float64 {
	return math.Float64frombits(DecoderUint64(b))
}

func WriterBytes(w io.Writer, id uint32, v []byte) (err error) {
	if 0 == len(v) {
		return
	}
	data := EncoderUint64(uint64(len(v)))
	err = WriterField(w, TBytes, uint16(id), uint8(len(data)))
	if err != nil {
		return
	}
	_, err = w.Write(data)
	if err != nil {
		return
	}
	_, err = w.Write(v)
	return
}

func WriterInt64(w io.Writer, id uint32, v int64) (err error) {
	if 0 == v {
		return
	}
	data := EncoderInt64(v)
	err = WriterField(w, TInt, uint16(id), uint8(len(data)))
	if err != nil {
		return
	}
	_, err = w.Write(data)
	if err != nil {
		return
	}
	return
}

func WriterUint64(w io.Writer, id uint32, v uint64) (err error) {
	if 0 == v {
		return
	}
	data := EncoderUint64(v)
	err = WriterField(w, TUint, uint16(id), uint8(len(data)))
	if err != nil {
		return
	}
	_, err = w.Write(data)
	if err != nil {
		return
	}
	return
}

func WriterFloat(w io.Writer, id uint32, v float32) (err error) {
	if 0 == v {
		return
	}
	data := EncoderFloat(v)
	err = WriterField(w, TFloat, uint16(id), uint8(len(data)))
	if err != nil {
		return
	}
	_, err = w.Write(data)
	if err != nil {
		return
	}
	return
}

func WriterDouble(w io.Writer, id uint32, v float64) (err error) {
	if 0 == v {
		return
	}
	data := EncoderDouble(v)
	err = WriterField(w, TDouble, uint16(id), uint8(len(data)))
	if err != nil {
		return
	}
	_, err = w.Write(data)
	if err != nil {
		return
	}
	return
}

func WriterList[E any](w io.Writer, id uint32, v []E, length func(v E) uint32, writer func(w io.Writer) error) (err error) {
	if 0 == len(v) {
		return
	}
	temp := uint32(0)
	for _, item := range v {
		temp += length(item)
	}

	data := EncoderUint64(uint64(len(v)))
	temp += uint32(len(data))

	buffer := EncoderUint64(uint64(temp))
	err = WriterField(w, TBytes, uint16(id), uint8(len(buffer)))
	if err != nil {
		return
	}

	_, err = w.Write(buffer)
	if err != nil {
		return
	}

	return
}

func ReaderFloat(typ Type, v any) (float32, error) {
	if typ == TFloat {
		return v.(float32), nil
	}
	if typ == TDouble {
		return float32(v.(float64)), nil
	}
	if typ == TInt {
		return float32(v.(int64)), nil
	}
	if typ == TUint {
		return float32(v.(uint64)), nil
	}
	return 0, errors.New("invalid Type")
}

func ReaderDouble(typ Type, v any) (float64, error) {
	if typ == TDouble {
		return v.(float64), nil
	}
	if typ == TFloat {
		return float64(v.(float32)), nil
	}
	if typ == TInt {
		return float64(v.(int64)), nil
	}
	if typ == TUint {
		return float64(v.(uint64)), nil
	}
	return 0, errors.New("invalid Type")
}

func ReaderBytes(typ Type, v any) ([]byte, error) {
	if typ == TBytes {
		return v.([]byte), nil
	}
	return nil, errors.New("invalid Type")
}

func ReaderString(typ Type, v any) (string, error) {
	if typ == TBytes {
		return string(v.([]byte)), nil
	}
	return "", errors.New("invalid Type")
}

func ReaderInt8(typ Type, v any) (int8, error) {
	if typ == TInt {
		return int8(v.(int64)), nil
	}
	if typ == TUint {
		return int8(v.(uint64)), nil
	}
	if typ == TFloat {
		return int8(v.(float32)), nil
	}
	if typ == TDouble {
		return int8(v.(float64)), nil
	}
	return 0, errors.New("invalid Type")
}

func ReaderInt16(typ Type, v any) (int16, error) {
	if typ == TInt {
		return int16(v.(int64)), nil
	}
	if typ == TUint {
		return int16(v.(uint64)), nil
	}
	if typ == TFloat {
		return int16(v.(float32)), nil
	}
	if typ == TDouble {
		return int16(v.(float64)), nil
	}
	return 0, errors.New("invalid Type")
}

func ReaderInt32(typ Type, v any) (int32, error) {
	if typ == TInt {
		return int32(v.(int64)), nil
	}
	if typ == TUint {
		return int32(v.(uint64)), nil
	}
	if typ == TFloat {
		return int32(v.(float32)), nil
	}
	if typ == TDouble {
		return int32(v.(float64)), nil
	}
	return 0, errors.New("invalid Type")
}

func ReaderInt64(typ Type, v any) (int64, error) {
	if typ == TInt {
		return int64(v.(int64)), nil
	}
	if typ == TUint {
		return int64(v.(uint64)), nil
	}
	if typ == TFloat {
		return int64(v.(float32)), nil
	}
	if typ == TDouble {
		return int64(v.(float64)), nil
	}
	return 0, errors.New("invalid Type")
}

func ReaderUint8(typ Type, v any) (uint8, error) {
	if typ == TUint {
		return uint8(v.(uint64)), nil
	}
	if typ == TUint {
		return uint8(v.(uint64)), nil
	}
	if typ == TFloat {
		return uint8(v.(float32)), nil
	}
	if typ == TDouble {
		return uint8(v.(float64)), nil
	}
	return 0, errors.New("invalid Type")
}

func ReaderUint16(typ Type, v any) (uint16, error) {
	if typ == TUint {
		return uint16(v.(uint64)), nil
	}
	if typ == TUint {
		return uint16(v.(uint64)), nil
	}
	if typ == TFloat {
		return uint16(v.(float32)), nil
	}
	if typ == TDouble {
		return uint16(v.(float64)), nil
	}
	return 0, errors.New("invalid Type")
}

func ReaderUint32(typ Type, v any) (uint32, error) {
	if typ == TUint {
		return uint32(v.(uint64)), nil
	}
	if typ == TUint {
		return uint32(v.(uint64)), nil
	}
	if typ == TFloat {
		return uint32(v.(float32)), nil
	}
	if typ == TDouble {
		return uint32(v.(float64)), nil
	}
	return 0, errors.New("invalid Type")
}

func ReaderUint64(typ Type, v any) (uint64, error) {
	if typ == TUint {
		return uint64(v.(uint64)), nil
	}
	if typ == TUint {
		return uint64(v.(uint64)), nil
	}
	if typ == TFloat {
		return uint64(v.(float32)), nil
	}
	if typ == TDouble {
		return uint64(v.(float64)), nil
	}
	return 0, errors.New("invalid Type")
}

func Decoder(r io.Reader, call func(typ Type, id uint16, value any) error) error {
	for {
		typ, id, valueLen, err := ReaderField(r)
		if err != nil {
			return nil
		}
		b := make([]byte, valueLen)
		count, err := r.Read(b)
		if err != nil {
			return err
		}
		if uint8(count) != valueLen {
			err = errors.New("Read fail, length error")
		}

		switch typ {
		case TInt:
			err = call(typ, id, DecoderInt64(b))
			if err != nil {
				return err
			}
		case TUint:
			err = call(typ, id, DecoderUint64(b))
			if err != nil {
				return err
			}
		case TFloat:
			err = call(typ, id, DecoderFloat(b))
			if err != nil {
				return err
			}
		case TDouble:
			err = call(typ, id, DecoderDouble(b))
			if err != nil {
				return err
			}
		case TBytes:
			bytesLength := DecoderUint64(b)
			data := make([]byte, bytesLength)
			count, err = r.Read(data)
			if err != nil {
				return err
			}
			if count != int(bytesLength) {
				err = errors.New("Read Bytes error")
			}
			err = call(typ, id, data)
			if err != nil {
				return err
			}
		case TList:
		case TMap:
		case TSet:
		case TData:
		case TExtend:
		default:
			return nil
		}
	}
}
