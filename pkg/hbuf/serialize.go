package hbuf

import (
	"bytes"
	"errors"
	"math"
)

type HbufType uint8

const (
	HInt   = HbufType(0)
	HUint  = HbufType(1)
	HFloat = HbufType(2)
	HBytes = HbufType(3)
	HList  = HbufType(4)
	HMap   = HbufType(5)
	HData  = HbufType(6)
)

func FormatTag(idLen uint8, typ HbufType, typLen uint8) uint8 {
	value := ((idLen - 1) & 3) << 6
	value |= (uint8(typ) & 7) << 3
	value |= uint8((typLen - 1) & 7)
	return value
}

func ParseTag(val uint8) (idLen uint8, typ HbufType, typLen uint8) {
	idLen = 1 + (val >> 6 & 3)
	typ = HbufType(val >> 3 & 7)
	typLen = 1 + (val & 7)
	return
}

func intToBytes(val int64) []byte {
	b := [8]byte{}
	i := 0
	for ; val != 0; i++ {
		b[i] = byte(val & 0xff)
		val = val >> 8
	}
	return b[:i]
}

func bytesToInt(b []byte, start uint32, len uint8) int64 {
	var val int64
	for i := 0; i < int(len); i++ {
		val = val<<8 | int64(b[int(start)+i])
	}
	return val
}

func FormatInt(buf *bytes.Buffer, id uint32, val int64) {
	FormatIntPrt(buf, id, &val)
}

func FormatIntPrt(buf *bytes.Buffer, id uint32, val *int64) {
	if nil == val {
		return
	}
	ids := intToBytes(int64(id))
	vals := intToBytes(*val)
	b := FormatTag(uint8(len(ids)), HInt, uint8(len(vals)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(vals)
}

func FormatUint(buf *bytes.Buffer, id uint32, val *uint64) {
	if nil == val {
		return
	}
	ids := intToBytes(int64(id))
	vals := intToBytes(int64(*val))
	b := FormatTag(uint8(len(ids)), HUint, uint8(len(vals)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(vals)
}

func FormatFloat(buf *bytes.Buffer, id uint32, val *float32) {
	if nil == val {
		return
	}
	ids := intToBytes(int64(id))
	vals := intToBytes(int64(math.Float32bits(*val)))
	b := FormatTag(uint8(len(ids)), HFloat, uint8(len(vals)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(vals)
}

func FormatDouble(buf *bytes.Buffer, id uint32, val *float64) {
	if nil == val {
		return
	}
	ids := intToBytes(int64(id))
	vals := intToBytes(int64(math.Float64bits(*val)))
	b := FormatTag(uint8(len(ids)), HFloat, uint8(len(vals)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(vals)
}

func FormatBytes(buf *bytes.Buffer, id uint32, val []byte) {
	if nil == val {
		return
	}
	ids := intToBytes(int64(id))
	lens := intToBytes(int64(len(val)))
	b := FormatTag(uint8(len(ids)), HBytes, uint8(len(lens)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(lens)
	buf.Write(val)
}

func FormatList[T any](buf *bytes.Buffer, id uint32, val []T, valCall func(buf *bytes.Buffer, id uint32, val T)) {
	if nil == val {
		return
	}
	buffer := bytes.Buffer{}
	FormatInt(&buffer, 0, int64(len(val)))
	for i, item := range val {
		valCall(&buffer, uint32(i), item)
	}

	ids := intToBytes(int64(id))
	lens := intToBytes(int64(buffer.Len()))
	b := FormatTag(uint8(len(ids)), HList, uint8(buffer.Len()))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(lens)
	_, _ = buffer.WriteTo(buf)
}

type MapKey interface {
	int | uint | int8 | uint8 | int16 | uint16 | int32 | uint32 | int64 | uint64 | float32 | float64 | string | bool |
		*int | *uint | *int8 | *uint8 | *int16 | *uint16 | *int32 | *uint32 | *int64 | *uint64 | *float32 | *float64 | *string | *bool |
		chan int | chan uint | chan int8 | chan uint8 | chan int16 | chan uint16 | chan int32 | chan uint32 | chan int64 | chan uint64 | chan float32 | chan float64 | chan string | chan bool |
		chan *int | chan *uint | chan *int8 | chan *uint8 | chan *int16 | chan *uint16 | chan *int32 | chan *uint32 | chan *int64 | chan *uint64 | chan *float32 | chan *float64 | chan *string | chan *bool
}

func FormatMap[K MapKey, V any](
	buf *bytes.Buffer, id uint32, val map[K]V, keyCall func(buf *bytes.Buffer, id uint32, val K), valCall func(buf *bytes.Buffer, id uint32, val V)) {
	if nil == val {
		return
	}
	ids := intToBytes(int64(id))
	lens := intToBytes(int64(len(val)))
	b := FormatTag(uint8(len(ids)), HMap, uint8(len(lens)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(lens)
	i := uint32(0)
	for key, item := range val {
		keyCall(buf, i, key)
		valCall(buf, i, item)
		i++
	}
}

func FormatData(buf *bytes.Buffer, id uint32, val Data) {
	if nil == val {
		return
	}
	data, err := val.ToData()
	if err != nil {
		return
	}

	ids := intToBytes(int64(id))
	lens := intToBytes(int64(len(data)))
	b := FormatTag(uint8(len(ids)), HData, uint8(len(lens)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(lens)
	buf.Write(data)
}

func Parse(buf []byte, start *uint32, len uint32, handles func(id uint32) ParseHandle) error {
	for *start < len {
		idLen, typ, typLen := ParseTag(buf[*start])
		*start++
		if uint32(idLen) > len-*start {
			return errors.New("data error")
		}
		id := bytesToInt(buf, *start, idLen)
		*start += uint32(idLen)
		if uint32(typLen) > len-*start {
			return errors.New("data error")
		}
		len := uint32(0)
		switch typ {
		case HInt:
			len = uint32(typLen)
		case HUint:
			len = uint32(typLen)
		case HFloat:
			len = uint32(typLen)
		case HBytes:
			len = uint32(bytesToInt(buf, *start, typLen))
			*start += uint32(typLen)
		case HList:
			len = uint32(bytesToInt(buf, *start, typLen))
			*start += uint32(typLen)
		case HMap:

		case HData:
			len = uint32(bytesToInt(buf, *start, typLen))
			*start += uint32(typLen)
		}
		if handle := handles(uint32(id)); nil != handle {
			err := handle(buf, *start, len, typ)
			if err != nil {
				return err
			}
			*start += len
		}
	}
	return nil
}

type ParseHandle func(buf []byte, start uint32, len uint32, typ HbufType) error

func ParseInt64(ret *int64) func(buf []byte, start uint32, len uint32, typ HbufType) error {
	return func(buf []byte, start uint32, len uint32, typ HbufType) error {
		*ret = bytesToInt(buf, start, uint8(len))
		return nil
	}
}

func ParseString(ret *string) func(buf []byte, start uint32, len uint32, typ HbufType) error {
	return func(buf []byte, start uint32, len uint32, typ HbufType) error {
		*ret = string(buf[start : start+len])
		return nil
	}
}

func ParseData(ret Data) func(buf []byte, start uint32, len uint32, typ HbufType) error {
	return func(buf []byte, start uint32, len uint32, typ HbufType) error {
		return ret.FormData(buf)
	}
}

func ParseList[T any](ret []T) func(buf []byte, start uint32, len uint32, typ HbufType) error {
	return func(buf []byte, start uint32, len uint32, typ HbufType) error {
		idLen, typ, typLen := ParseTag(buf[start])
		start += 1 + uint32(idLen)
		if uint32(typLen) > len-start {
			return errors.New("data error")
		}
		number := bytesToInt(buf, start, typLen)
		for i := 0; i < int(number); i++ {

		}
		return nil
	}
}
