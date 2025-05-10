package hbuf

import (
	"errors"
	"io"
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
		v = int64(int8(b[0]))
	} else if valueLen == 2 {
		v = int64(b[0]) + int64(int8(b[1]))<<8
	} else if valueLen == 3 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(int8(b[2]))<<16
	} else if valueLen == 4 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(b[2])<<16 + int64(int8(b[3]))<<24
	} else if valueLen == 5 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(b[2])<<16 + int64(b[3])<<24 + int64(int8(b[4]))<<32
	} else if valueLen == 6 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(b[2])<<16 + int64(b[3])<<24 + int64(b[4])<<32 + int64(int8(b[5]))<<40
	} else if valueLen == 7 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(b[2])<<16 + int64(b[3])<<24 + int64(b[4])<<32 + int64(b[5])<<40 + int64(int8(b[6]))<<48
	} else if valueLen == 8 {
		v = int64(b[0]) + int64(b[1])<<8 + int64(b[2])<<16 + int64(b[3])<<24 + int64(b[4])<<32 + int64(b[5])<<40 + int64(b[6])<<48 + int64(int8(b[7]))<<56
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
