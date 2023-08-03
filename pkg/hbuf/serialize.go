package hbuf

import (
	"bytes"
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

func ParseTag(val uint8) (idLen uint8, typ uint8, typLen uint8) {
	idLen = 1 + (val >> 6 & 3)
	typ = val >> 3 & 7
	typLen = 1 + (val & 7)
	return
}

func IntToBytes(val int64) []byte {
	b := [8]byte{}
	i := 0
	for ; val != 0; i++ {
		b[i] = byte(val & 0xff)
		val = val >> 8
	}
	return b[:i]
}

func FormatInt(buf *bytes.Buffer, id int32, val *int64) {
	if nil == val {
		return
	}
	ids := IntToBytes(int64(id))
	vals := IntToBytes(*val)
	b := FormatTag(uint8(len(ids)), HInt, uint8(len(vals)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(vals)
}

func FormatUint(buf *bytes.Buffer, id int32, val *uint64) {
	if nil == val {
		return
	}
	ids := IntToBytes(int64(id))
	vals := IntToBytes(int64(*val))
	b := FormatTag(uint8(len(ids)), HUint, uint8(len(vals)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(vals)
}

func FormatFloat(buf *bytes.Buffer, id int32, val *float32) {
	if nil == val {
		return
	}
	ids := IntToBytes(int64(id))
	vals := IntToBytes(int64(math.Float32bits(*val)))
	b := FormatTag(uint8(len(ids)), HFloat, uint8(len(vals)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(vals)
}

func FormatDouble(buf *bytes.Buffer, id int32, val *float64) {
	if nil == val {
		return
	}
	ids := IntToBytes(int64(id))
	vals := IntToBytes(int64(math.Float64bits(*val)))
	b := FormatTag(uint8(len(ids)), HFloat, uint8(len(vals)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(vals)
}

func FormatBytes(buf *bytes.Buffer, id int32, val []byte) {
	if nil == val {
		return
	}
	ids := IntToBytes(int64(id))
	lens := IntToBytes(int64(len(val)))
	b := FormatTag(uint8(len(ids)), HBytes, uint8(len(lens)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(lens)
	buf.Write(val)
}

func FormatList[T any](buf *bytes.Buffer, id int32, val []T, valCall func(buf *bytes.Buffer, id int32, val T)) {
	if nil == val {
		return
	}
	ids := IntToBytes(int64(id))
	lens := IntToBytes(int64(len(val)))
	b := FormatTag(uint8(len(ids)), HList, uint8(len(lens)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(lens)
	for i, item := range val {
		valCall(buf, int32(i), item)
	}
}

type MapKey interface {
	int | uint | int8 | uint8 | int16 | uint16 | int32 | uint32 | int64 | uint64 | float32 | float64 | string | bool |
		*int | *uint | *int8 | *uint8 | *int16 | *uint16 | *int32 | *uint32 | *int64 | *uint64 | *float32 | *float64 | *string | *bool |
		chan int | chan uint | chan int8 | chan uint8 | chan int16 | chan uint16 | chan int32 | chan uint32 | chan int64 | chan uint64 | chan float32 | chan float64 | chan string | chan bool |
		chan *int | chan *uint | chan *int8 | chan *uint8 | chan *int16 | chan *uint16 | chan *int32 | chan *uint32 | chan *int64 | chan *uint64 | chan *float32 | chan *float64 | chan *string | chan *bool
}

func FormatMap[K MapKey, V any](
	buf *bytes.Buffer, id int32, val map[K]V, keyCall func(buf *bytes.Buffer, id int32, val K), valCall func(buf *bytes.Buffer, id int32, val V)) {
	if nil == val {
		return
	}
	ids := IntToBytes(int64(id))
	lens := IntToBytes(int64(len(val)))
	b := FormatTag(uint8(len(ids)), HMap, uint8(len(lens)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(lens)
	i := int32(0)
	for key, item := range val {
		keyCall(buf, i, key)
		valCall(buf, i, item)
		i++
	}
}

func FormatData(buf *bytes.Buffer, id int32, val Data) {
	if nil == val {
		return
	}
	data, err := val.ToData()
	if err != nil {
		return
	}

	ids := IntToBytes(int64(id))
	lens := IntToBytes(int64(len(data)))
	b := FormatTag(uint8(len(ids)), HData, uint8(len(lens)))
	buf.WriteByte(b)
	buf.Write(ids)
	buf.Write(lens)
	buf.Write(data)
}
