package hbuf_test

import ()

//
//func TestDecoderUnt64(t *testing.T) {
//	for i := uint64(0); i < 0xFFFFFFFF; i++ {
//		b := hbuf.EncoderUint64(i)
//		value := hbuf.DecoderUint64(b)
//		if value != i {
//			t.Fatal("Decoder int64 error type = " + strconv.Itoa(int(value)))
//		}
//	}
//}
//
//func TestDecoderInt64(t *testing.T) {
//	count := uint64(0)
//	for i := int64(-0x800000) - 1; i < int64(0x800000)+1; i++ {
//		b := hbuf.EncoderInt64(int64(i))
//		value := hbuf.DecoderInt64(b)
//		if int64(value) != i {
//			t.Fatal("Decoder int64 error type = " + strconv.Itoa(int(i)))
//		}
//		count++
//	}
//	t.Log(count)
//	t.Log(0xFFFFFFFF)
//
//	tests := []int64{0x80000000, 0x7FFFFFFF, -0x80000000, -0x7FFFFFFF,
//		0x123456789ABCDEF, -0x123456789ABCDEF, 0x123456789ABCDEF0, -0x123456789ABCDEF0,
//		0x80000000 + 1, 0x80000000, 0x80000000 - 1, 0x7FFFFFFF + 1, 0x7FFFFFFF, 0x7FFFFFFF - 1,
//		-0x80000000 + 1, -0x80000000, -0x80000000 - 1, -0x7FFFFFFF + 1, -0x7FFFFFFF, -0x7FFFFFFF - 1,
//		0x80000000000000, -0x80000000000000, 0x80000000000000 + 1, 0x80000000000000 - 1,
//		0x7FFFFFFFFFFFFF, -0x7FFFFFFFFFFFFF, 0x7FFFFFFFFFFFFF + 1, 0x7FFFFFFFFFFFFF - 1, -0x7FFFFFFFFFFFFF + 1, -0x7FFFFFFFFFFFFF - 1,
//	}
//	for i, _ := range tests {
//		b := hbuf.EncoderInt64(tests[i])
//		value := hbuf.DecoderInt64(b)
//		if value != tests[i] {
//			t.Fatal("Decoder int64 error type = " + strconv.Itoa(int(i)))
//		}
//	}
//}
