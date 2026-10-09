package hbuf

import (
	"encoding/binary"
	"errors"
	"time"
)

var ErrShortBuffer = errors.New("hbuf: short buffer")

func DecodeType(buf []byte) (typ Type, idLen uint8, valueLen uint8, ret []byte, err error) {
	if len(buf) < 1 {
		return 0, 0, 0, buf, ErrShortBuffer
	}
	b := buf[0]
	typ = Type(b >> 5 & 0b111)
	valueLen = (b >> 2 & 0b111) + 1
	idLen = b & 0b11
	ret = buf[1:]
	return typ, idLen, valueLen, ret, nil
}

func DecodeId(buf []byte, idLen uint8) (id uint16, ret []byte, err error) {
	switch idLen {
	case 0:
		return 0, buf, nil
	case 1:
		if len(buf) < 1 {
			return 0, buf, ErrShortBuffer
		}
		return uint16(buf[0]), buf[1:], nil
	case 2:
		if len(buf) < 2 {
			return 0, buf, ErrShortBuffer
		}
		return uint16(buf[0]) + uint16(buf[1])<<8, buf[2:], nil
	}
	return 0, buf, ErrShortBuffer
}

func DecodeInt64(buf []byte, valueLen uint8) (int64, []byte, error) {
	if valueLen == 0 || valueLen > 8 || len(buf) < int(valueLen) {
		return 0, buf, ErrShortBuffer
	}
	if valueLen == 1 {
		return int64(int8(buf[0])), buf[1:], nil
	} else if valueLen == 2 {
		return int64(buf[0]) + int64(int8(buf[1]))<<8, buf[2:], nil
	} else if valueLen == 3 {
		return int64(buf[0]) + int64(buf[1])<<8 + int64(int8(buf[2]))<<16, buf[3:], nil
	} else if valueLen == 4 {
		return int64(buf[0]) + int64(buf[1])<<8 + int64(buf[2])<<16 + int64(int8(buf[3]))<<24, buf[4:], nil
	} else if valueLen == 5 {
		return int64(buf[0]) + int64(buf[1])<<8 + int64(buf[2])<<16 + int64(buf[3])<<24 + int64(int8(buf[4]))<<32, buf[5:], nil
	} else if valueLen == 6 {
		return int64(buf[0]) + int64(buf[1])<<8 + int64(buf[2])<<16 + int64(buf[3])<<24 + int64(buf[4])<<32 + int64(int8(buf[5]))<<40, buf[6:], nil
	} else if valueLen == 7 {
		return int64(buf[0]) + int64(buf[1])<<8 + int64(buf[2])<<16 + int64(buf[3])<<24 + int64(buf[4])<<32 + int64(buf[5])<<40 + int64(int8(buf[6]))<<48, buf[7:], nil
	}
	return int64(buf[0]) + int64(buf[1])<<8 + int64(buf[2])<<16 + int64(buf[3])<<24 + int64(buf[4])<<32 + int64(buf[5])<<40 + int64(buf[6])<<48 + int64(int8(buf[7]))<<56, buf[8:], nil
}

func DecodeUint64(buf []byte, valueLen uint8) (uint64, []byte, error) {
	if valueLen == 0 || valueLen > 8 || len(buf) < int(valueLen) {
		return 0, buf, ErrShortBuffer
	}
	if valueLen == 1 {
		return uint64(buf[0]), buf[1:], nil
	} else if valueLen == 2 {
		return uint64(buf[0]) + uint64(buf[1])<<8, buf[2:], nil
	} else if valueLen == 3 {
		return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16, buf[3:], nil
	} else if valueLen == 4 {
		return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16 + uint64(buf[3])<<24, buf[4:], nil
	} else if valueLen == 5 {
		return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16 + uint64(buf[3])<<24 + uint64(buf[4])<<32, buf[5:], nil
	} else if valueLen == 6 {
		return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16 + uint64(buf[3])<<24 + uint64(buf[4])<<32 + uint64(buf[5])<<40, buf[6:], nil
	} else if valueLen == 7 {
		return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16 + uint64(buf[3])<<24 + uint64(buf[4])<<32 + uint64(buf[5])<<40 + uint64(buf[6])<<48, buf[7:], nil
	}
	return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16 + uint64(buf[3])<<24 + uint64(buf[4])<<32 + uint64(buf[5])<<40 + uint64(buf[6])<<48 + uint64(buf[7])<<56, buf[8:], nil
}

// DecodeTime 从 9 字节中安全解包并还原为 time.Time
func DecodeTime(buf []byte) (time.Time, []byte, error) {
	if len(buf) < 9 {
		return time.Time{}, buf, ErrShortBuffer
	}
	low64 := binary.LittleEndian.Uint64(buf[0:8])
	high8 := uint64(buf[8])
	nsec := low64 & 0x3FFFFFFF
	secs := (high8 << 34) | (low64 >> 30)
	return time.Unix(int64(secs), int64(nsec)).In(time.Local), buf[9:], nil
}
