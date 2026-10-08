package hbuf

import "time"

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

// EncodeTime 将时间转换为（42位秒 + 30位纳秒）的 72 位大整数，
// 并采用你完全相同的小端序（低位在前）风格写入 buf 中。
func EncodeTime(buf []byte, t *time.Time) []byte {
	secs := uint64(t.Unix())
	nsec := uint64(t.Nanosecond())

	// ================== 位域拼接 (42位秒 + 30位纳秒) ==================
	// 1. 低 64 位安全融合：
	//    因为低 30 位给纳秒，所以高 34 位给秒数。
	//    我们先用 (secs & 0x3FFFFFFFF) 提取出 secs 的低 34 位，再左移 30 位。
	//    这样操作绝对不会引发 uint64 溢出。
	low64 := ((secs & 0x3FFFFFFFF) << 30) | (nsec & 0x3FFFFFFF)

	// 2. 高 8 位提取：
	//    secs 的总长度是 42 位，低 34 位已经放进了 low64。
	//    剩下的最高 8 位（42 - 34 = 8），正好通过右移 34 位提取出来。
	high8 := byte(secs >> 34)

	// 3. 严格遵循你的小端序变长追加风格
	return append(buf,
		byte(low64),
		byte(low64>>8),
		byte(low64>>16),
		byte(low64>>24),
		byte(low64>>32),
		byte(low64>>40),
		byte(low64>>48),
		byte(low64>>56),
		high8, // 追加第 9 个字节（高位秒数）
	)
}
