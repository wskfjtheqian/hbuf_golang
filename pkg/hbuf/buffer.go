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
类型 Type|值的长| uint64(01234567 + 1 ) 	| ID长度 uint16(012 + 1)
	111		|1| 111			| 1
TAG	|PTR| LEN | ID | VALUE
------------------------------------------------
当 TBytes  的 TAG
类型 Type	|是否是指针| 长度值的长度 uint64(01234567 + 1 ) 	| ID长度 uint16(012 + 1)
	111		|1| 111				| 11
TAG	|PTR| ID | LEN | VALUE
------------------------------------------------
当 TList  的 TAG
类型 Type	| 数量值的长 uint64	| ID长度 uint32
	111		| 111				| 11
TAG	| ID | COUNT | <VALUE | <VALUE> |...>
------------------------------------------------
当 TMap  的 TAG
类型 Type	| 是否有KEY	| 数量值的长 uint64 	| ID长度 uint32
	111		| 1			| 11				| 11
TAG	| ID | COUNT | <<KEY> | VALUE | <<KEY> | VALUE> | ...>
------------------------------------------------
当 TData  的 TAG
类型 Type	| 是否有Extend	| 数量值的长 uint64 	| ID长度 uint32
	111		| 1				| 11				| 11
TAG	| ID | COUNT | <EXTEND_COUNT | Extend| <> | ...> | <FIELD ...>
------------------------------------------------

*/

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

// WriterField 写入 Field
func WriterField(w io.Writer, typ Type, isNull bool, id uint16, valueLen uint8) (err error) {
	var b []byte
	var idLen uint8
	if id <= 0xFF {
		idLen = 1
		b = make([]byte, 2)
		b[1] = byte(id)
	} else {
		idLen = 2
		b = make([]byte, 3)
		b[1] = byte(id)
		b[2] = byte(id >> 8)
	}

	null := 0
	if isNull {
		null = 1
	}

	b[0] = byte(typ&0b111) << 5
	b[0] |= byte(null&0b1) << 4
	b[0] |= byte(int(valueLen-1)&0b111) << 1
	b[0] |= byte(idLen-1) & 0b1

	_, err = w.Write(b)
	return
}

// ReaderField 读取 Field
func ReaderField(r io.Reader) (typ Type, isNull bool, id uint16, valueLen uint8, err error) {
	var count int
	b := make([]byte, 1)
	count, err = r.Read(b)
	if err != nil {
		return
	}
	if count != 1 {
		err = errors.New("read fail, length error")
	}
	idLen := (b[0] & 0b1) + 1
	valueLen = (b[0] >> 1 & 0b111) + 1
	isNull = (b[0] >> 4 & 0b1) == 1
	typ = Type(b[0] >> 5 & 0b111)

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
	return
}

// EncoderInt64 编码 int64 类型
func EncoderInt64(v int64) (b []byte) {
	if v >= -0x80 && v < 0x80 {
		b = make([]byte, 1)
		b[0] = byte(v)
	} else if v >= -0x8000 && v < 0x8000 {
		b = make([]byte, 2)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
	} else if v >= -0x800000 && v < 0x800000 {
		b = make([]byte, 3)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
	} else if v >= -0x80000000 && v < 0x80000000 {
		b = make([]byte, 4)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
	} else if v >= -0x8000000000 && v < 0x8000000000 {
		b = make([]byte, 5)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
		b[4] = byte(v >> 32)
	} else if v >= -0x800000000000 && v < 0x800000000000 {
		b = make([]byte, 6)
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
		b[4] = byte(v >> 32)
		b[5] = byte(v >> 40)
	} else if v >= -0x80000000000000 && v < 0x80000000000000 {
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

// DecoderInt64 解码 int64 类型
func DecoderInt64(b []byte) int64 {
	length := len(b)
	if length == 1 {
		return int64(int8(b[0]))
	} else if length == 2 {
		return int64(b[0]) | int64(int8(b[1]))<<8
	} else if length == 3 {
		return int64(b[0]) | int64(b[1])<<8 | int64(int8(b[2]))<<16
	} else if length == 4 {
		return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(int8(b[3]))<<24
	} else if length == 5 {
		return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(b[3])<<24 | int64(int8(b[4]))<<32
	} else if length == 6 {
		return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(b[3])<<24 | int64(b[4])<<32 | int64(int8(b[5]))<<40
	} else if length == 7 {
		return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(b[3])<<24 | int64(b[4])<<32 | int64(b[5])<<40 | int64(int8(b[6]))<<48
	} else if length == 8 {
		return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(b[3])<<24 | int64(b[4])<<32 | int64(b[5])<<40 | int64(b[6])<<48 | int64(int8(b[7]))<<56
	}
	return 0
}

// LengthInt64 计算 int64 类型长度
func LengthInt64(v int64) (length uint8) {
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

// EncoderUint64 编码 uint64 类型
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

// DecoderUint64 解码 uint64 类型
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

// LengthUint64 计算 uint64 类型长度
func LengthUint64(v uint64) (length uint8) {
	if v <= 0xFF {
		length = 1
	} else if v <= 0xFFFF {
		length = 2
	} else if v <= 0xFFFFFF {
		length = 3
	} else if v <= 0xFFFFFFFF {
		length = 4
	} else if v <= 0xFFFFFFFFFF {
		length = 5
	} else if v <= 0xFFFFFFFFFFFF {
		length = 6
	} else if v <= 0xFFFFFFFFFFFFFF {
		length = 7
	} else {
		length = 8
	}
	return
}

func LengthFloat(v float32) (length uint8) {
	return LengthUint64(uint64(math.Float32bits(v)))
}

func LengthDouble(v float64) (length uint8) {
	return LengthUint64(math.Float64bits(v))
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

func LengthBytes(v []byte) (length uint32) {
	length = uint32(len(v))
	length += uint32(LengthUint64(uint64(length)))
	return length
}

func WriterBool(w io.Writer, id uint16, v bool) (err error) {
	if !v {
		return
	}
	err = WriterField(w, TBool, true, id, 1)
	if err != nil {
		return err
	}
	return
}

func WriterBytes(w io.Writer, id uint16, v []byte) (err error) {
	if 0 == len(v) {
		return
	}
	data := EncoderUint64(uint64(len(v)))
	err = WriterField(w, TBytes, true, id, uint8(len(data)))
	if err != nil {
		return
	}
	_, err = w.Write(data)
	if err != nil {
		return err
	}
	_, err = w.Write(v)
	return
}

func WriterData(w io.Writer, id uint16, v Data) (err error) {
	if v == nil {
		return
	}

	size := v.Size()
	data := EncoderUint64(uint64(size))
	err = WriterField(w, TData, true, id, uint8(len(data)))
	if err != nil {
		return
	}
	_, err = w.Write(data)
	if err != nil {
		return err
	}
	err = v.Encoder(w)
	if err != nil {
		return err
	}
	return
}

func WriterList[E any](w io.Writer, id uint16, v []E, length func(v E) uint32, writer func(w io.Writer, v E) error) (err error) {
	if 0 == len(v) {
		return
	}
	size := uint32(0)
	for _, item := range v {
		size += length(item)
	}

	count := len(v)
	countData := EncoderUint64(uint64(count))
	size += 2 + uint32(len(countData))

	data := EncoderUint64(uint64(size))
	err = WriterField(w, TList, true, id, uint8(len(data)))
	if err != nil {
		return
	}
	_, err = w.Write(data)
	if err != nil {
		return err
	}

	err = WriterField(w, TUint, true, 0, uint8(len(countData)))
	if err != nil {
		return
	}
	_, err = w.Write(countData)
	if err != nil {
		return err
	}

	for _, item := range v {
		err = writer(w, item)
		if err != nil {
			return err
		}
	}
	return nil
}

func WriterInt64(w io.Writer, id uint16, v int64) (err error) {
	if 0 == v {
		return
	}
	data := EncoderInt64(v)
	err = WriterField(w, TInt, true, id, uint8(len(data)))
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	if err != nil {
		return
	}
	return
}

func WriterUint64(w io.Writer, id uint16, v uint64) (err error) {
	if 0 == v {
		return
	}
	data := EncoderUint64(v)
	err = WriterField(w, TUint, true, id, uint8(len(data)))
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	if err != nil {
		return
	}
	return
}

func WriterFloat(w io.Writer, id uint16, v float32) (err error) {
	if 0 == v {
		return
	}
	data := EncoderFloat(v)
	err = WriterField(w, TFloat, true, id, uint8(len(data)))
	if err != nil {
		return err
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
	err = WriterField(w, TFloat, true, uint16(id), uint8(len(data)))
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	if err != nil {
		return
	}
	return
}

type Number interface {
	int | int8 | int16 | int32 | int64 | uint | uint8 | uint16 | uint32 | uint64 | float32 | float64
}

func ReaderNumber[T Number](v any) (T, error) {
	switch v.(type) {
	case int:
		return T(v.(int)), nil
	case int8:
		return T(v.(int8)), nil
	case int16:
		return T(v.(int16)), nil
	case int32:
		return T(v.(int32)), nil
	case int64:
		return T(v.(int64)), nil
	case uint:
		return T(v.(uint)), nil
	case uint8:
		return T(v.(uint8)), nil
	case uint16:
		return T(v.(uint16)), nil
	case uint32:
		return T(v.(uint32)), nil
	case uint64:
		return T(v.(uint64)), nil
	case float32:
		return T(v.(float32)), nil
	case float64:
		return T(v.(float64)), nil
	default:
		return 0, errors.New("invalid Type")
	}
}

type Bytes interface {
	[]byte | string
}

func ReaderBytes[T Bytes](v any) (T, error) {
	switch v.(type) {
	case []byte:
		return T(v.([]byte)), nil
	case string:
		return T(v.(string)), nil
	default:
		ret := new(T)
		return *ret, errors.New("invalid Type")
	}
}

func ReaderBool(v any) (bool, error) {
	switch v.(type) {
	case bool:
		return v.(bool), nil
	default:
		return false, errors.New("invalid Type")
	}
}

func ReaderData(v any, out Data) error {
	r, ok := v.(io.Reader)
	if !ok {
		return errors.New("invalid Type")
	}
	err := out.Decoder(r)
	if err != nil {
		return err
	}
	return nil
}

func ReaderList[E any](v any, reader func(v any) (E, error)) ([]E, error) {
	r, ok := v.(io.Reader)
	if !ok {
		return nil, errors.New("invalid Type")
	}
	typ, _, id, valueLen, err := ReaderField(r)
	if err != nil {
		return nil, err
	}
	if id != 0 {
		return nil, errors.New("invalid id")
	}
	b := make([]byte, valueLen)
	size, err := r.Read(b)
	if err != nil {
		return nil, err
	}
	if uint8(size) != valueLen {
		return nil, errors.New("read fail, length error")
	}

	count := DecoderInt64(b)
	list := make([]E, count)
	for i := 0; i < int(count); i++ {
		typ, _, id, valueLen, err = ReaderField(r)
		if err != nil {
			return nil, err
		}
		if id != 0 {
			return nil, errors.New("invalid id")
		}

		b := make([]byte, valueLen)
		size, err = r.Read(b)
		if err != nil {
			return nil, err
		}
		if uint8(size) != valueLen {
			return nil, errors.New("read fail, length error")
		}
		switch typ {
		case TInt:
			list[i], err = reader(DecoderInt64(b))
			if err != nil {
				return nil, err
			}
		case TUint:
			list[i], err = reader(DecoderUint64(b))
			if err != nil {
				return nil, err
			}
		case TFloat:
			if len(b) == 4 {
				list[i], err = reader(DecoderFloat(b))
				if err != nil {
					return nil, err
				}
			} else if len(b) == 8 {
				list[i], err = reader(DecoderDouble(b))
				if err != nil {
					return nil, err
				}
			} else {
				return nil, errors.New("invalid float length")
			}
		case TBytes:
			bytesLength := DecoderUint64(b)
			data := make([]byte, bytesLength)
			size, err = r.Read(data)
			if err != nil && err != io.EOF {
				return nil, err
			}
			if size != int(bytesLength) {
				err = errors.New("read Bytes error")
			}
			list[i], err = reader(data)
			if err != nil {
				return nil, err
			}
		case TData:
			list[i], err = reader(NewEndReader(r, DecoderUint64(b)))
			if err != nil {
				return nil, err
			}
		case TList:
			list[i], err = reader(NewEndReader(r, DecoderUint64(b)))
			if err != nil {
				return nil, err
			}
		case TMap:

		default:
			return nil, errors.New("invalid Type")
		}
	}
	return list, nil
}

func Decoder(r io.Reader, call func(typ Type, id uint16, value any) error) (err error) {
	for {
		var size int
		var typ Type
		var id uint16
		var valueLen uint8

		typ, _, id, valueLen, err = ReaderField(r)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if typ == TBool {
			err = call(typ, id, valueLen != 0)
			if err != nil {
				return err
			}
			continue
		}
		b := make([]byte, valueLen)
		size, err = r.Read(b)
		if err != nil {
			return err
		}
		if uint8(size) != valueLen {
			return errors.New("read fail, length error")
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
			if len(b) == 4 {
				err = call(typ, id, DecoderFloat(b))
			} else if len(b) == 8 {
				err = call(typ, id, DecoderDouble(b))
			} else {
				return errors.New("invalid float length")
			}
		case TBytes:
			bytesLength := DecoderUint64(b)
			data := make([]byte, bytesLength)
			size, err = r.Read(data)
			if err != nil && err != io.EOF {
				return err
			}
			if size != int(bytesLength) {
				err = errors.New("read Bytes error")
			}
			err = call(typ, id, data)
			if err != nil {
				return err
			}
		case TData:
			err = call(TData, id, NewEndReader(r, DecoderUint64(b)))
			if err != nil {
				return err
			}
		case TList:
			err = call(TList, id, NewEndReader(r, DecoderUint64(b)))
			if err != nil {
				return err
			}
		case TMap:

		default:
			return errors.New("invalid Type")
		}
	}
}

func NewEndReader(r io.Reader, end uint64) *EndReader {
	return &EndReader{end: end, Reader: r}
}

type EndReader struct {
	io.Reader
	end uint64
}

func (r *EndReader) Read(p []byte) (n int, err error) {
	if r.end <= 0 {
		return 0, io.EOF
	}
	if uint64(len(p)) > r.end {
		p = p[:r.end]
	}
	n, err = r.Reader.Read(p)
	if err != nil {
		return n, err
	}
	r.end -= uint64(n)
	return
}
