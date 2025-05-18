package hbuf

func DecodeType(buf []byte) (typ Type, idLen uint8, valueLen uint8, ret []byte) {
	b := buf[0]
	typ = Type(b >> 5 & 0b111)
	valueLen = (b >> 2 & 0b111) + 1
	idLen = b & 0b11

	ret = buf[1:]
	return
}

func DecodeId(buf []byte, idLen uint8) (uint16, []byte) {
	if idLen == 1 {
		return uint16(int8(buf[0])), buf[1:]
	} else {
		return uint16(buf[0]) + uint16(int8(buf[1]))<<8, buf[2:]
	}
}

func DecodeInt64(buf []byte, valueLen uint8) (int64, []byte) {
	if valueLen == 1 {
		return int64(int8(buf[0])), buf[1:]
	} else if valueLen == 2 {
		return int64(buf[0]) + int64(int8(buf[1]))<<8, buf[2:]
	} else if valueLen == 3 {
		return int64(buf[0]) + int64(buf[1])<<8 + int64(int8(buf[2]))<<16, buf[3:]
	} else if valueLen == 4 {
		return int64(buf[0]) + int64(buf[1])<<8 + int64(buf[2])<<16 + int64(int8(buf[3]))<<24, buf[4:]
	} else if valueLen == 5 {
		return int64(buf[0]) + int64(buf[1])<<8 + int64(buf[2])<<16 + int64(buf[3])<<24 + int64(int8(buf[4]))<<32, buf[5:]
	} else if valueLen == 6 {
		return int64(buf[0]) + int64(buf[1])<<8 + int64(buf[2])<<16 + int64(buf[3])<<24 + int64(buf[4])<<32 + int64(int8(buf[5]))<<40, buf[6:]
	} else if valueLen == 7 {
		return int64(buf[0]) + int64(buf[1])<<8 + int64(buf[2])<<16 + int64(buf[3])<<24 + int64(buf[4])<<32 + int64(buf[5])<<40 + int64(int8(buf[6]))<<48, buf[7:]
	} else {
		return int64(buf[0]) + int64(buf[1])<<8 + int64(buf[2])<<16 + int64(buf[3])<<24 + int64(buf[4])<<32 + int64(buf[5])<<40 + int64(buf[6])<<48 + int64(int8(buf[7]))<<56, buf[8:]
	}
}

func DecodeUint64(buf []byte, valueLen uint8) (uint64, []byte) {
	if valueLen == 1 {
		return uint64(buf[0]), buf[1:]
	} else if valueLen == 2 {
		return uint64(buf[0]) + uint64(buf[1])<<8, buf[2:]
	} else if valueLen == 3 {
		return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16, buf[3:]
	} else if valueLen == 4 {
		return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16 + uint64(buf[3])<<24, buf[4:]
	} else if valueLen == 5 {
		return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16 + uint64(buf[3])<<24 + uint64(buf[4])<<32, buf[5:]
	} else if valueLen == 6 {
		return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16 + uint64(buf[3])<<24 + uint64(buf[4])<<32 + uint64(buf[5])<<40, buf[6:]
	} else if valueLen == 7 {
		return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16 + uint64(buf[3])<<24 + uint64(buf[4])<<32 + uint64(buf[5])<<40 + uint64(buf[6])<<48, buf[7:]
	} else {
		return uint64(buf[0]) + uint64(buf[1])<<8 + uint64(buf[2])<<16 + uint64(buf[3])<<24 + uint64(buf[4])<<32 + uint64(buf[5])<<40 + uint64(buf[6])<<48 + uint64(buf[7])<<56, buf[8:]
	}
}
