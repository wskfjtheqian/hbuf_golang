package hbuf

func WriterType(buf []byte, typ Type, idLen uint8, valueLen uint8) []byte {
	return append(buf, byte(typ&0b111)<<5|byte(int(valueLen-1)&0b111)<<2|byte(idLen)&0b11)
}

func LengthId(h uint16) uint8 {
	if h <= 0xFF {
		return 1
	} else {
		return 2
	}
}

func WriterId(buf []byte, v uint16) []byte {
	if v <= 0xFF {
		return append(buf, byte(v))
	} else {
		return append(buf, byte(v), byte(v>>8))
	}
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

func WriterInt64(buf []byte, v int64) []byte {
	if v >= -0x80 && v < 0x80 {
		return append(buf, byte(v))
	} else if v >= -0x8000 && v < 0x8000 {
		return append(buf, byte(v), byte(v>>8))
	} else if v >= -0x800000 && v < 0x800000 {
		return append(buf, byte(v), byte(v>>8), byte(v>>16))
	} else if v >= -0x80000000 && v < 0x80000000 {
		return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	} else if v >= -0x8000000000 && v < 0x8000000000 {
		return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24), byte(v>>32))
	} else if v >= -0x800000000000 && v < 0x800000000000 {
		return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24), byte(v>>32), byte(v>>40))
	} else if v >= -0x80000000000000 && v < 0x80000000000000 {
		return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24), byte(v>>32), byte(v>>40), byte(v>>48))
	} else {
		return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24), byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56))
	}
}

func EncodeUint64(buf []byte, v uint64) []byte {
	if v <= 0xFF {
		return append(buf, byte(v))
	} else if v <= 0xFFFF {
		return append(buf, byte(v), byte(v>>8))
	} else if v <= 0xFFFFFF {
		return append(buf, byte(v), byte(v>>8), byte(v>>16))
	} else if v <= 0xFFFFFFFF {
		return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	} else if v <= 0xFFFFFFFFFF {
		return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24), byte(v>>32))
	} else if v <= 0xFFFFFFFFFFFF {
		return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24), byte(v>>32), byte(v>>40))
	} else if v <= 0xFFFFFFFFFFFFFF {
		return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24), byte(v>>32), byte(v>>40), byte(v>>48))
	} else {
		return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24), byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56))
	}
}
