package hbuf

import (
	"io"
)

func WriterTypeId(writer io.Writer, typ Type, id uint16, valueLen uint8) (err error) {
	if id == 0 {
		_, err = writer.Write([]byte{
			byte(typ&0b111)<<5 | byte(int(valueLen-1)&0b111)<<2, //|byte(0) & 0b11
		})
	} else if id <= 0xFF {
		_, err = writer.Write([]byte{
			byte(typ&0b111)<<5 | byte(int(valueLen-1)&0b111)<<2 | byte(1)&0b11,
			byte(id),
		})
	} else {
		_, err = writer.Write([]byte{
			byte(typ&0b111)<<5 | byte(int(valueLen-1)&0b111)<<2 | byte(2)&0b11,
			byte(id),
			byte(id >> 8),
		})
	}
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
