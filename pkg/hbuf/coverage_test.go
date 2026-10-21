package hbuf_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/shopspring/decimal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
)

// ================= 测试专用类型 =================

var crossType CrossTypeTest
var crossTypeDesc = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(&crossType), map[uint16]hbuf.Descriptor{}, map[uint16]hbuf.Descriptor{
	1: hbuf.NewInt64Descriptor(unsafe.Offsetof(crossType.V1), false),
	2: hbuf.NewStringDescriptor(unsafe.Offsetof(crossType.V2), false),
	3: hbuf.NewBytesDescriptor(unsafe.Offsetof(crossType.V3), false),
	4: hbuf.NewFloatDescriptor(unsafe.Offsetof(crossType.V4), false),
	5: hbuf.NewDecimalDescriptor(unsafe.Offsetof(crossType.V5), false),
	6: hbuf.NewUint64Descriptor(unsafe.Offsetof(crossType.V6), false),
})

type CrossTypeTest struct {
	V1 int64
	V2 string
	V3 []byte
	V4 float32
	V5 decimal.Decimal
	V6 uint64
}

func (g *CrossTypeTest) Descriptors() hbuf.Descriptor { return crossTypeDesc }

var twoByteId TwoByteIdTest
var twoByteIdDesc = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(&twoByteId), map[uint16]hbuf.Descriptor{}, map[uint16]hbuf.Descriptor{
	256:   hbuf.NewInt64Descriptor(unsafe.Offsetof(twoByteId.V1), false),
	300:   hbuf.NewStringDescriptor(unsafe.Offsetof(twoByteId.V2), false),
	65535: hbuf.NewInt32Descriptor(unsafe.Offsetof(twoByteId.V3), true),
	257:   hbuf.NewBoolDescriptor(unsafe.Offsetof(twoByteId.V4), false),
	500:   hbuf.NewBytesDescriptor(unsafe.Offsetof(twoByteId.V5), false),
})

type TwoByteIdTest struct {
	V1 int64
	V2 string
	V3 *int32
	V4 bool
	V5 []byte
}

func (g *TwoByteIdTest) Descriptors() hbuf.Descriptor { return twoByteIdDesc }

var bigList BigListTest
var bigListDesc = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(&bigList), map[uint16]hbuf.Descriptor{}, map[uint16]hbuf.Descriptor{
	1: hbuf.NewListDescriptor[int16](unsafe.Offsetof(bigList.V1), hbuf.NewInt16Descriptor(0, false), false),
	2: hbuf.NewMapDescriptor[int64, int16](unsafe.Offsetof(bigList.V2), hbuf.NewInt64Descriptor(0, false), hbuf.NewInt16Descriptor(0, false), false),
	3: hbuf.NewListDescriptor[uint8](unsafe.Offsetof(bigList.V3), hbuf.NewUint8Descriptor(0, false), false),
	4: hbuf.NewListDescriptor[int16](unsafe.Offsetof(bigList.V4), hbuf.NewInt16Descriptor(0, false), true),
	5: hbuf.NewMapDescriptor[int64, int16](unsafe.Offsetof(bigList.V5), hbuf.NewInt64Descriptor(0, false), hbuf.NewInt16Descriptor(0, false), true),
})

type BigListTest struct {
	V1 []int16
	V2 map[int64]int16
	V3 []uint8
	V4 *[]int16
	V5 *map[int64]int16
}

func (g *BigListTest) Descriptors() hbuf.Descriptor { return bigListDesc }

var deepTest DeepTest
var deepDesc = hbuf.NewSyncDescriptor(func() hbuf.Descriptor {
	return hbuf.NewDataDescriptor(unsafe.Offsetof(deepTest.V1), false, reflect.TypeOf(&deepTest), map[uint16]hbuf.Descriptor{}, map[uint16]hbuf.Descriptor{
		1: hbuf.NewListDescriptor[*DeepTest](unsafe.Offsetof(deepTest.V1), hbuf.NewRefDescriptor(&DeepTest{}, 0, true), false),
		3: hbuf.NewRefDescriptor(&DeepTest{}, unsafe.Offsetof(deepTest.V3), true),
	})
})

type DeepTest struct {
	V1 []*DeepTest
	V3 *DeepTest
}

func (g *DeepTest) Descriptors() hbuf.Descriptor { return deepDesc.Desc() }

// ================= 工具 =================

// wrapData 将字段字节包进完整 Data 结构（count 个字段 + 无扩展 EXTENDS-BLOCK）。
func wrapData(t *testing.T, fields []byte, count uint64) []byte {
	t.Helper()
	buf := hbuf.WriterType(nil, hbuf.TData, 1, hbuf.LengthUint(count))
	buf = hbuf.EncodeUint64(buf, count)
	buf = append(buf, fields...)
	return hbuf.WriterType(buf, hbuf.TInt, 0, 1)
}

// field 构造一个字段：TAG | ID | VALUE（按 typ 类型 + 原始 value 字节）。
func field(typ hbuf.Type, id uint16, value ...byte) []byte {
	buf := hbuf.WriterType(nil, typ, hbuf.LengthId(id), hbuf.LengthUint(uint64(len(value))))
	buf = hbuf.WriterId(buf, id)
	return append(buf, value...)
}

// fieldInt 构造一个 TInt 字段，value 按变长编码。
func fieldInt(id uint16, v int64) []byte {
	buf := hbuf.WriterType(nil, hbuf.TInt, hbuf.LengthId(id), hbuf.LengthInt(v))
	buf = hbuf.WriterId(buf, id)
	return hbuf.WriterInt64(buf, v)
}

// fieldStr 构造一个 TBytes 字段（LEN | DATA）。
func fieldStr(id uint16, s string) []byte {
	buf := hbuf.WriterType(nil, hbuf.TBytes, hbuf.LengthId(id), hbuf.LengthUint(uint64(len(s))))
	buf = hbuf.WriterId(buf, id)
	buf = hbuf.EncodeUint64(buf, uint64(len(s)))
	return append(buf, s...)
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// ================= 1. 编码原语 =================

func TestEncodePrimitives(t *testing.T) {
	lu := []struct {
		v    uint64
		want uint8
	}{{0, 1}, {0xFF, 1}, {0x100, 2}, {0xFFFF, 2}, {0x10000, 3}, {0xFFFFFF, 3},
		{0x1000000, 4}, {0xFFFFFFFF, 4}, {0x100000000, 5}, {0xFFFFFFFFFF, 5},
		{0x10000000000, 6}, {0xFFFFFFFFFFFF, 6}, {0x1000000000000, 7}, {0xFFFFFFFFFFFFFF, 7},
		{0x100000000000000, 8}, {math.MaxUint64, 8}}
	for _, c := range lu {
		if got := hbuf.LengthUint(c.v); got != c.want {
			t.Errorf("LengthUint(%#x) = %d, want %d", c.v, got, c.want)
		}
	}

	li := []struct {
		v    int64
		want uint8
	}{{0, 1}, {127, 1}, {-128, 1}, {128, 2}, {-129, 2}, {32767, 2}, {-32768, 2},
		{32768, 3}, {-32769, 3}, {0x7FFFFF, 3}, {-0x800000, 3}, {0x800000, 4}, {-0x800001, 4},
		{0x7FFFFFFF, 4}, {-0x80000000, 4}, {0x80000000, 5}, {-0x80000001, 5},
		{0x7FFFFFFFFF, 5}, {-0x8000000000, 5}, {0x8000000000, 6}, {-0x8000000001, 6},
		{0x7FFFFFFFFFFF, 6}, {-0x800000000000, 6}, {0x800000000000, 7}, {-0x800000000001, 7},
		{0x7FFFFFFFFFFFFF, 7}, {-0x80000000000000, 7}, {math.MaxInt64, 8}, {math.MinInt64, 8}}
	for _, c := range li {
		if got := hbuf.LengthInt(c.v); got != c.want {
			t.Errorf("LengthInt(%d) = %d, want %d", c.v, got, c.want)
		}
	}

	lid := []struct {
		v    uint16
		want uint8
	}{{0, 1}, {0xFF, 1}, {0x100, 2}, {0xFFFF, 2}}
	for _, c := range lid {
		if got := hbuf.LengthId(c.v); got != c.want {
			t.Errorf("LengthId(%d) = %d, want %d", c.v, got, c.want)
		}
	}

	eu := []struct {
		v    uint64
		want string
	}{{0, "00"}, {255, "ff"}, {256, "0001"}, {65535, "ffff"}, {65536, "000001"},
		{16777215, "ffffff"}, {4294967295, "ffffffff"}, {4294967296, "0000000001"},
		{math.MaxUint64, "ffffffffffffffff"}}
	for _, c := range eu {
		got := hbuf.EncodeUint64(nil, c.v)
		if hex.EncodeToString(got) != c.want {
			t.Errorf("EncodeUint64(%d) = %x, want %s", c.v, got, c.want)
		}
	}

	wi := []struct {
		v    int64
		want string
	}{{0, "00"}, {127, "7f"}, {-128, "80"}, {128, "8000"}, {-129, "7fff"},
		{-1, "ff"}, {math.MinInt64, "0000000000000080"}, {math.MaxInt64, "ffffffffffffff7f"}}
	for _, c := range wi {
		got := hbuf.WriterInt64(nil, c.v)
		if hex.EncodeToString(got) != c.want {
			t.Errorf("WriterInt64(%d) = %x, want %s", c.v, got, c.want)
		}
	}
}

// ================= 2. 解码原语 =================

func TestDecodePrimitives(t *testing.T) {
	di := []struct {
		hex  string
		want int64
	}{{"7f", 127}, {"80", -128}, {"ff", -1}, {"ffff", -1}, {"7f00", 127},
		{"0080", -32768}, {"ff7f", 32767}, {"ffffffffffffff7f", math.MaxInt64}, {"0000000000000080", math.MinInt64}}
	for _, c := range di {
		v, rest, err := hbuf.DecodeInt64(mustHex(c.hex), uint8(len(c.hex)/2))
		if err != nil || v != c.want || len(rest) != 0 {
			t.Errorf("DecodeInt64(%s) = %d, rest=%d, err=%v, want %d", c.hex, v, len(rest), err, c.want)
		}
	}

	du := []struct {
		hex  string
		want uint64
	}{{"ff", 255}, {"0001", 256}, {"ffff", 65535}, {"000001", 65536},
		{"ffffffff", 4294967295}, {"ffffffffffffffff", math.MaxUint64}}
	for _, c := range du {
		v, rest, err := hbuf.DecodeUint64(mustHex(c.hex), uint8(len(c.hex)/2))
		if err != nil || v != c.want || len(rest) != 0 {
			t.Errorf("DecodeUint64(%s) = %d, rest=%d, err=%v, want %d", c.hex, v, len(rest), err, c.want)
		}
	}
}

func TestDecodePrimitiveErrors(t *testing.T) {
	// DecodeType 空 buf
	if _, _, _, _, err := hbuf.DecodeType(nil); !errors.Is(err, hbuf.ErrShortBuffer) {
		t.Errorf("DecodeType(nil) err = %v, want ErrShortBuffer", err)
	}
	// DecodeId 截断
	if _, _, err := hbuf.DecodeId(nil, 1); !errors.Is(err, hbuf.ErrShortBuffer) {
		t.Errorf("DecodeId(nil,1) err = %v, want ErrShortBuffer", err)
	}
	if _, _, err := hbuf.DecodeId([]byte{1}, 2); !errors.Is(err, hbuf.ErrShortBuffer) {
		t.Errorf("DecodeId(1B,2) err = %v, want ErrShortBuffer", err)
	}
	// DecodeInt64/DecodeUint64 非法长度与截断
	for _, l := range []uint8{0, 9} {
		if _, _, err := hbuf.DecodeInt64(nil, l); !errors.Is(err, hbuf.ErrShortBuffer) {
			t.Errorf("DecodeInt64(nil,%d) err = %v, want ErrShortBuffer", l, err)
		}
		if _, _, err := hbuf.DecodeUint64(nil, l); !errors.Is(err, hbuf.ErrShortBuffer) {
			t.Errorf("DecodeUint64(nil,%d) err = %v, want ErrShortBuffer", l, err)
		}
	}
	if _, _, err := hbuf.DecodeInt64([]byte{1}, 2); !errors.Is(err, hbuf.ErrShortBuffer) {
		t.Errorf("DecodeInt64(1B,2) err = %v, want ErrShortBuffer", err)
	}
	// DecodeTime 截断
	if _, _, err := hbuf.DecodeTime(make([]byte, 8)); !errors.Is(err, hbuf.ErrShortBuffer) {
		t.Errorf("DecodeTime(8B) err = %v, want ErrShortBuffer", err)
	}
}

// ================= 3. 变长整数边界 round-trip =================

func TestIntBoundaryRoundTrip(t *testing.T) {
	src := HBufTest{}
	// 列表元素覆盖 1~8 字节转折点（零值元素以空标记编码，这里全非零）
	src.V39 = []int64{127, 128, -1, -128, -129, 255, 256, -255, -256, -257,
		32767, 32768, -32767, -32768, -32769, 65535, 65536,
		16777215, 16777216, -16777216, -16777217,
		4294967295, 4294967296, -4294967296, -4294967297,
		281474976710655, math.MaxInt64, math.MinInt64}
	src.V47 = []uint64{math.MaxUint64, 1 << 63, (1 << 63) - 1, 4294967296, 65536, 256, 1, 72057594037927935}
	// 字段级极限
	src.V7 = math.MaxInt64
	src.V8 = new(int64(math.MinInt64))
	src.V15 = math.MaxUint64

	buf, err := hbuf.Marshal(&src)
	if err != nil {
		t.Fatal(err)
	}
	var des HBufTest
	if err = hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	if !slicesEqI64(des.V39, src.V39) {
		t.Errorf("V39 round-trip mismatch:\n got %v\nwant %v", des.V39, src.V39)
	}
	if !slicesEqU64(des.V47, src.V47) {
		t.Errorf("V47 round-trip mismatch:\n got %v\nwant %v", des.V47, src.V47)
	}
	if des.V7 != src.V7 || des.V8 == nil || *des.V8 != *src.V8 || des.V15 != src.V15 {
		t.Errorf("field boundary mismatch: %d %v %d", des.V7, des.V8, des.V15)
	}
}

func slicesEqI64(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func slicesEqU64(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ================= 4. 2 字节 ID =================

func TestTwoByteId(t *testing.T) {
	src := TwoByteIdTest{
		V1: math.MaxInt64,
		V2: "hello 2-byte id",
		V3: new(int32(123)),
		V4: true,
		V5: []byte{1, 2, 3},
	}
	buf, err := hbuf.Marshal(&src)
	if err != nil {
		t.Fatal(err)
	}
	var des TwoByteIdTest
	if err = hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	if des.V1 != src.V1 || des.V2 != src.V2 || des.V3 == nil || *des.V3 != *src.V3 ||
		des.V4 != src.V4 || !bytes.Equal(des.V5, src.V5) {
		t.Errorf("2-byte id round-trip mismatch:\n got %+v\nwant %+v", des, src)
	}
	// 字节级：TAG(TInt, idLen=2, valueLen=8)=0x1E | ID 256 小端 0x00 0x01
	if !bytes.Contains(buf, []byte{0x1E, 0x00, 0x01}) {
		t.Errorf("id 256 bytes = %x, want contain 1e0001", buf)
	}
}

// ================= 5. uint8 零值特例 =================

func TestUint8ZeroSpecial(t *testing.T) {
	// 字段级：值 0 恒省略（含 *uint8 指向 0）
	z := HBufTest{V9: 0}
	buf, _ := hbuf.Marshal(&z)
	if bytes.Contains(buf, []byte{9}) {
		t.Errorf("V9=0 should be omitted, buf=%x", buf)
	}
	p0 := HBufTest{V10: new(uint8(0))}
	buf, _ = hbuf.Marshal(&p0)
	if bytes.Contains(buf, []byte{10}) {
		t.Errorf("V10=&0 should be omitted, buf=%x", buf)
	}
	// 非零可空 uint8 写入
	one := HBufTest{V10: new(uint8(1))}
	buf, _ = hbuf.Marshal(&one)
	if !bytes.Contains(buf, []byte{10}) {
		t.Errorf("V10=&1 should be written, buf=%x", buf)
	}

	// 元素级：零值元素以 (TUint, idLen=2, 1) 空标记编码（无 VALUE），非零正常编码
	lst := HBufTest{V41: []uint8{0, 1, 255}}
	buf, _ = hbuf.Marshal(&lst)
	// 手动解码字段 41 的字节：TAG(TUint,1,1)=0x21 | id=0x29 | count=3 | 元素
	// 元素 0：TAG(TUint,2,1)=0x22（无 VALUE）；元素 1：TAG(TUint,1,1)=0x21|0x01；元素 255：0x21|0xFF
	want := []byte{0xA1, 41, 3, 0x22, 0x21, 1, 0x21, 255}
	if !bytes.Contains(buf, want) {
		t.Errorf("V41={0,1,255} encoding mismatch:\n got %x\nwant contain %x", buf, want)
	}
	var des HBufTest
	if err := hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	if len(des.V41) != 3 || des.V41[0] != 0 || des.V41[1] != 1 || des.V41[2] != 255 {
		t.Errorf("V41 round-trip mismatch: %v", des.V41)
	}

	// nil 指针元素空标记 idLen=0：(TUint, 0, 1)=0x20；非 nil 指向 0：(TUint, 2, 1)=0x22
	lst2 := HBufTest{V42: []*uint8{nil, new(uint8(0)), new(uint8(1))}}
	buf, _ = hbuf.Marshal(&lst2)
	want2 := []byte{0xA1, 42, 3, 0x20, 0x22, 0x21, 1}
	if !bytes.Contains(buf, want2) {
		t.Errorf("V42={nil,&0,&1} encoding mismatch:\n got %x\nwant contain %x", buf, want2)
	}
	var des2 HBufTest
	if err := hbuf.Unmarshal(buf, &des2); err != nil {
		t.Fatal(err)
	}
	if len(des2.V42) != 3 || des2.V42[0] != nil || des2.V42[1] == nil || *des2.V42[1] != 0 || des2.V42[2] == nil || *des2.V42[2] != 1 {
		t.Errorf("V42 round-trip mismatch: %v", des2.V42)
	}
}

// ================= 6. 跨类型兼容解码 =================

func TestCrossTypeDecode(t *testing.T) {
	// int64 字段接受 TBool / TInt / TUint / TFloat / TBytes
	intCases := []struct {
		name   string
		fields []byte
		want   int64
	}{
		{"bool-true", func() []byte {
			buf := hbuf.WriterType(nil, hbuf.TBool, 1, 2)
			return hbuf.WriterId(buf, 1)
		}(), 1},
		{"bool-false", func() []byte {
			buf := hbuf.WriterType(nil, hbuf.TBool, 1, 1)
			return hbuf.WriterId(buf, 1)
		}(), 0},
		{"int", fieldInt(1, -42), -42},
		{"uint", func() []byte {
			buf := hbuf.WriterType(nil, hbuf.TUint, 1, hbuf.LengthUint(42))
			buf = hbuf.WriterId(buf, 1)
			return hbuf.EncodeUint64(buf, 42)
		}(), 42},
		{"float", func() []byte {
			bits := math.Float64bits(3.5)
			buf := hbuf.WriterType(nil, hbuf.TFloat, 1, hbuf.LengthUint(bits))
			buf = hbuf.WriterId(buf, 1)
			return hbuf.EncodeUint64(buf, bits)
		}(), 3},
		{"bytes", fieldStr(1, "123"), 123},
	}
	for _, c := range intCases {
		buf := wrapData(t, c.fields, 1)
		var des HBufSubTest
		if err := hbuf.Unmarshal(buf, &des); err != nil {
			t.Errorf("%s: err=%v", c.name, err)
			continue
		}
		if des.A1 != c.want {
			t.Errorf("%s: A1=%d, want %d", c.name, des.A1, c.want)
		}
	}

	// string / bytes / decimal / float / uint64 字段接受数值类输入
	strCases := []struct {
		name   string
		id     uint16
		fields []byte
		want   string
	}{
		{"string<-int", 2, fieldInt(2, 42), "42"},
		{"string<-uint", 2, func() []byte {
			buf := hbuf.WriterType(nil, hbuf.TUint, 1, 1)
			buf = hbuf.WriterId(buf, 2)
			return hbuf.EncodeUint64(buf, 42)
		}(), "42"},
		{"string<-float", 2, func() []byte {
			bits := math.Float64bits(3.5)
			buf := hbuf.WriterType(nil, hbuf.TFloat, 1, hbuf.LengthUint(bits))
			buf = hbuf.WriterId(buf, 2)
			return hbuf.EncodeUint64(buf, bits)
		}(), "3.5"},
		{"string<-bool", 2, func() []byte {
			buf := hbuf.WriterType(nil, hbuf.TBool, 1, 2)
			return hbuf.WriterId(buf, 2)
		}(), "true"},
		{"decimal<-bytes", 5, fieldStr(5, "12.34"), "12.34"},
		{"decimal<-int", 5, fieldInt(5, 42), "42"},
	}
	for _, c := range strCases {
		buf := wrapData(t, c.fields, 1)
		var des CrossTypeTest
		if err := hbuf.Unmarshal(buf, &des); err != nil {
			t.Errorf("%s: err=%v", c.name, err)
			continue
		}
		var got string
		switch c.id {
		case 2:
			got = des.V2
		case 5:
			got = des.V5.String()
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	// float32 字段接受 TInt
	fbuf := wrapData(t, fieldInt(4, 42), 1)
	var fdes CrossTypeTest
	if err := hbuf.Unmarshal(fbuf, &fdes); err != nil || fdes.V4 != 42 {
		t.Errorf("float<-int: got %v err=%v, want 42", fdes.V4, err)
	}
	// uint64 字段接受 TBool 与 TBytes
	ubuf := wrapData(t, func() []byte {
		buf := hbuf.WriterType(nil, hbuf.TBool, 1, 2)
		return hbuf.WriterId(buf, 6)
	}(), 1)
	var udes CrossTypeTest
	if err := hbuf.Unmarshal(ubuf, &udes); err != nil || udes.V6 != 1 {
		t.Errorf("uint<-bool: got %v err=%v, want 1", udes.V6, err)
	}
	ubuf2 := wrapData(t, fieldStr(6, "123"), 1)
	if err := hbuf.Unmarshal(ubuf2, &udes); err != nil || udes.V6 != 123 {
		t.Errorf("uint<-bytes: got %v err=%v, want 123", udes.V6, err)
	}
}

// ================= 7. 未知字段跳过 =================

func TestUnknownFieldSkip(t *testing.T) {
	unknownList := func() []byte {
		buf := hbuf.WriterType(nil, hbuf.TList, hbuf.LengthId(11), 1)
		buf = hbuf.WriterId(buf, 11)
		buf = hbuf.EncodeUint64(buf, 3)
		buf = hbuf.WriterType(buf, hbuf.TInt, 1, 1)
		buf = append(buf, 1)
		buf = hbuf.WriterType(buf, hbuf.TInt, 1, 1)
		buf = append(buf, 2)
		buf = hbuf.WriterType(buf, hbuf.TInt, 1, 1)
		buf = append(buf, 3)
		return buf
	}
	unknownMap := func() []byte {
		buf := hbuf.WriterType(nil, hbuf.TMap, hbuf.LengthId(12), 1)
		buf = hbuf.WriterId(buf, 12)
		buf = hbuf.EncodeUint64(buf, 1)
		// key: TInt 1
		buf = hbuf.WriterType(buf, hbuf.TInt, 1, 1)
		buf = append(buf, 7)
		// value: TBytes "v"
		buf = hbuf.WriterType(buf, hbuf.TBytes, 1, 1)
		buf = hbuf.EncodeUint64(buf, 1)
		buf = append(buf, 'v')
		return buf
	}
	unknownData := func() []byte {
		inner := fieldInt(99, 999) // 嵌套 data 的未知字段
		inner = append(inner, hbuf.WriterType(nil, hbuf.TInt, 0, 1)...)
		buf := hbuf.WriterType(nil, hbuf.TData, hbuf.LengthId(13), 1)
		buf = hbuf.WriterId(buf, 13)
		buf = hbuf.EncodeUint64(buf, 1)
		buf = append(buf, inner...)
		return buf
	}

	fields := append([]byte{}, fieldInt(9, 42)...)    // 未知 TInt
	fields = append(fields, fieldStr(10, "hello")...) // 未知 TBytes
	fields = append(fields, unknownList()...)         // 未知 TList
	fields = append(fields, unknownMap()...)          // 未知 TMap
	fields = append(fields, unknownData()...)         // 未知 TData（含嵌套未知字段）
	fields = append(fields, fieldInt(1, 99)...)       // 已知 A1
	fields = append(fields, fieldInt(2, -5)...)       // 已知 A2
	buf := wrapData(t, fields, 7)

	var des HBufSubTest
	if err := hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	if des.A1 != 99 || des.A2 == nil || *des.A2 != -5 {
		t.Errorf("unknown field skip: got A1=%d A2=%v, want 99 / -5", des.A1, des.A2)
	}
}

// ================= 8. 浮点特殊值 =================

func TestFloatSpecial(t *testing.T) {
	nan32 := float32(math.NaN())
	nan64 := math.NaN()
	inf := float32(math.Inf(1))
	ninf := float32(math.Inf(-1))
	src := HBufTest{
		V23: nan32,
		V24: &inf,
		V25: nan64,
		V26: new(float64(math.Inf(-1))),
		V55: []float32{nan32, inf, ninf},
		V57: []float64{nan64, math.Inf(1), math.Inf(-1)},
		V87: map[int64]float32{1: nan32, 2: inf},
		V89: map[int64]float64{1: nan64, 2: math.Inf(-1)},
	}
	buf, err := hbuf.Marshal(&src)
	if err != nil {
		t.Fatal(err)
	}
	var des HBufTest
	if err = hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	// float32 位模式级比较（NaN payload 保真）
	if math.Float32bits(des.V23) != math.Float32bits(nan32) {
		t.Errorf("V23 NaN bits = %08x, want %08x", math.Float32bits(des.V23), math.Float32bits(nan32))
	}
	if des.V24 == nil || math.Float32bits(*des.V24) != math.Float32bits(inf) {
		t.Errorf("V24 mismatch: %v", des.V24)
	}
	if math.Float64bits(des.V25) != math.Float64bits(nan64) {
		t.Errorf("V25 NaN bits = %016x, want %016x", math.Float64bits(des.V25), math.Float64bits(nan64))
	}
	if des.V26 == nil || math.IsNaN(*des.V26) || *des.V26 != math.Inf(-1) {
		t.Errorf("V26 mismatch: %v", des.V26)
	}
	if len(des.V55) != 3 || !math.IsNaN(float64(des.V55[0])) || des.V55[1] != inf || des.V55[2] != ninf {
		t.Errorf("V55 mismatch: %v", des.V55)
	}
	if len(des.V57) != 3 || !math.IsNaN(des.V57[0]) || des.V57[1] != math.Inf(1) || des.V57[2] != math.Inf(-1) {
		t.Errorf("V57 mismatch: %v", des.V57)
	}
	if !math.IsNaN(float64(des.V87[1])) || des.V87[2] != inf {
		t.Errorf("V87 mismatch: %v", des.V87)
	}
	if !math.IsNaN(des.V89[1]) || des.V89[2] != math.Inf(-1) {
		t.Errorf("V89 mismatch: %v", des.V89)
	}

	// 字段级 -0 省略（值比较判空）；元素级 -0 按位模式保真
	neg0 := HBufTest{V23: float32(math.Copysign(0, -1)), V25: math.Copysign(0, -1)}
	buf, _ = hbuf.Marshal(&neg0)
	if bytes.Contains(buf, []byte{23}) || bytes.Contains(buf, []byte{25}) {
		t.Errorf("field -0 should be omitted, buf=%x", buf)
	}
	elNeg0 := HBufTest{V55: []float32{float32(math.Copysign(0, -1))}, V57: []float64{math.Copysign(0, -1)}}
	buf, _ = hbuf.Marshal(&elNeg0)
	var des2 HBufTest
	if err = hbuf.Unmarshal(buf, &des2); err != nil {
		t.Fatal(err)
	}
	if math.Float32bits(des2.V55[0]) != 0x80000000 {
		t.Errorf("V55 -0 bits = %08x, want 80000000", math.Float32bits(des2.V55[0]))
	}
	if math.Float64bits(des2.V57[0]) != 0x8000000000000000 {
		t.Errorf("V57 -0 bits = %016x, want 8000000000000000", math.Float64bits(des2.V57[0]))
	}
}

// ================= 9. 整数溢出截断与 float32 精度 =================

func TestIntOverflowAndFloatPrecision(t *testing.T) {
	// int8 字段解码 300 → 44（按位截断）；int8 负溢出
	cases := []struct {
		name   string
		id     uint16
		fields []byte
		want   int64
	}{
		{"int8-300", 1, func() []byte {
			buf := hbuf.WriterType(nil, hbuf.TInt, 1, 2)
			buf = hbuf.WriterId(buf, 1)
			return append(buf, 0x2C, 0x01) // 300
		}(), 44},
		{"int8-300-uint", 9, func() []byte {
			buf := hbuf.WriterType(nil, hbuf.TUint, 1, 2)
			buf = hbuf.WriterId(buf, 9)
			return hbuf.EncodeUint64(buf, 300)
		}(), 44},
	}
	for _, c := range cases {
		buf := wrapData(t, c.fields, 1)
		var des HBufTest
		if err := hbuf.Unmarshal(buf, &des); err != nil {
			t.Errorf("%s: err=%v", c.name, err)
			continue
		}
		var got int64
		if c.id == 1 {
			got = int64(des.V1)
		} else {
			got = int64(des.V9)
		}
		if got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}

	// float32 字段解码 float64 位模式 → 截断为 float32 精度
	pi64 := math.Float64bits(math.Pi)
	fbuf := wrapData(t, func() []byte {
		buf := hbuf.WriterType(nil, hbuf.TFloat, 1, hbuf.LengthUint(pi64))
		buf = hbuf.WriterId(buf, 23)
		return hbuf.EncodeUint64(buf, pi64)
	}(), 1)
	var fdes HBufTest
	if err := hbuf.Unmarshal(fbuf, &fdes); err != nil {
		t.Fatal(err)
	}
	if math.Float32bits(fdes.V23) != math.Float32bits(float32(math.Pi)) {
		t.Errorf("float32 precision: got %08x, want %08x", math.Float32bits(fdes.V23), math.Float32bits(float32(math.Pi)))
	}
}

// ================= 10. 深层递归嵌套 =================

func TestDeepNested(t *testing.T) {
	d3 := &DeepTest{}
	d2 := &DeepTest{V1: []*DeepTest{nil, d3, {}}}
	d1 := &DeepTest{V1: []*DeepTest{d2, nil}, V3: d3}

	buf, err := hbuf.Marshal(d1)
	if err != nil {
		t.Fatal(err)
	}
	var des DeepTest
	if err = hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	if len(des.V1) != 2 || des.V1[0] == nil || des.V1[1] != nil {
		t.Fatalf("V1 mismatch: %+v", des.V1)
	}
	if len(des.V1[0].V1) != 3 || des.V1[0].V1[0] != nil || des.V1[0].V1[1] == nil {
		t.Fatalf("V1[0].V1 mismatch: %+v", des.V1[0].V1)
	}
	if des.V3 == nil {
		t.Fatalf("V3 should not be nil")
	}
	// nil 元素与零值结构元素：解码后均为非 nil（空标记 → 新实例 / 空结构）
	if des.V1[0].V1[2] == nil {
		t.Fatalf("V1[0].V1[2] zero-struct element should decode to non-nil")
	}
}

// ================= 11. 大 count（>255 元素） =================

func TestBigCount(t *testing.T) {
	src := BigListTest{}
	for i := 0; i < 256; i++ {
		src.V1 = append(src.V1, int16(i))
	}
	src.V2 = make(map[int64]int16, 300)
	for i := 0; i < 300; i++ {
		src.V2[int64(i)] = int16(i % 256)
	}
	src.V3 = make([]uint8, 300)
	for i := range src.V3 {
		src.V3[i] = uint8(i % 256)
	}

	buf, err := hbuf.Marshal(&src)
	if err != nil {
		t.Fatal(err)
	}
	var des BigListTest
	if err = hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	if len(des.V1) != 256 || des.V1[0] != 0 || des.V1[255] != 255 {
		t.Errorf("V1 mismatch: len=%d first=%d last=%d", len(des.V1), des.V1[0], des.V1[255])
	}
	if len(des.V2) != 300 || des.V2[299] != 43 {
		t.Errorf("V2 mismatch: len=%d V2[299]=%d", len(des.V2), des.V2[299])
	}
	if len(des.V3) != 300 || des.V3[299] != 43 {
		t.Errorf("V3 mismatch: len=%d V3[299]=%d", len(des.V3), des.V3[299])
	}
	// count 256 的变长编码为 2 字节（0x00 0x01）
	// 字段 1 字节序列：TAG(TList,1,1)=0x41 | id=1 | count(2B)=0x00 0x01
	if !bytes.Contains(buf, []byte{0xA5, 1, 0x00, 0x01}) {
		t.Errorf("V1 count 256 should encode as 2 bytes, buf prefix=%x", buf[:8])
	}
}

// ================= 12. 全零与空值 =================

func TestZeroAndEmpty(t *testing.T) {
	// 全零 data 的最小编码：TAG(TData,1,1) | 0x00 | EXTENDS(TInt,0,1)
	z := HBufTest{}
	buf, err := hbuf.Marshal(&z)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(buf) != "e101e11f000000" {
		t.Errorf("zero data encoding = %x", buf)
	}
	var des HBufTest
	if err = hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(z, des) {
		t.Errorf("zero round-trip mismatch")
	}

	// 空集合字段（零值）省略；非 nil 空集合指针仍写入
	empty := HBufTest{V33: []int8{}, V53: [][]byte{}, V65: map[int64]int8{}, V97: map[string]int8{}}
	buf, _ = hbuf.Marshal(&empty)
	for _, id := range []byte{33, 53, 65, 97} {
		if bytes.Contains(buf, []byte{id}) {
			t.Errorf("empty collection field %d should be omitted, buf=%x", id, buf)
		}
	}
	// 非 nil 空列表指针（可空列表字段）：写入 TAG(TList,1,1)=0x41 | id=4 | count=0
	emptyLst := []int16{}
	pEmpty := BigListTest{V4: &emptyLst}
	buf, _ = hbuf.Marshal(&pEmpty)
	if !bytes.Contains(buf, []byte{0xA1, 4, 0}) {
		t.Errorf("non-nil empty list pointer should be written, buf=%x", buf)
	}
	// nil 列表指针省略
	pNil := BigListTest{}
	buf, _ = hbuf.Marshal(&pNil)
	if bytes.Contains(buf, []byte{0x41, 4}) {
		t.Errorf("nil list pointer should be omitted, buf=%x", buf)
	}

	// 空串 / 空 bytes / 空 bytes 指针
	str := HBufTest{V19: "", V20: new(""), V21: []byte{}, V22: new([]byte{})}
	buf, _ = hbuf.Marshal(&str)
	if bytes.Contains(buf, []byte{19}) || bytes.Contains(buf, []byte{21}) {
		t.Errorf("empty string/bytes should be omitted, buf=%x", buf)
	}
	if !bytes.Contains(buf, []byte{20}) || !bytes.Contains(buf, []byte{22}) {
		t.Errorf("non-nil empty string/bytes pointer should be written, buf=%x", buf)
	}

	// Unmarshal 空 buf / nil buf 不报错
	if err = hbuf.Unmarshal(nil, &des); err != nil {
		t.Errorf("Unmarshal(nil) err=%v, want nil", err)
	}
	if err = hbuf.Unmarshal([]byte{}, &des); err != nil {
		t.Errorf("Unmarshal(empty) err=%v, want nil", err)
	}
}

// ================= 13. 解码错误路径 =================

func TestDecodeErrors(t *testing.T) {
	var des HBufTest
	// 截断：声明 count=5 但无字段
	truncated := []byte{hbuf.WriterType(nil, hbuf.TData, 1, 1)[0], 5}
	if err := hbuf.Unmarshal(truncated, &des); err == nil {
		t.Errorf("truncated data should error")
	}
	// 仅 TAG
	if err := hbuf.Unmarshal([]byte{hbuf.WriterType(nil, hbuf.TData, 1, 1)[0]}, &des); err == nil {
		t.Errorf("tag-only data should error")
	}
	// 列表元素截断：count=2 只有 1 个元素
	lbuf := wrapData(t, func() []byte {
		buf := hbuf.WriterType(nil, hbuf.TList, 1, 1)
		buf = hbuf.WriterId(buf, 33)
		buf = hbuf.EncodeUint64(buf, 2)
		buf = hbuf.WriterType(buf, hbuf.TInt, 1, 1)
		return append(buf, 1)
	}(), 1)
	if err := hbuf.Unmarshal(lbuf, &des); err == nil {
		t.Errorf("truncated list should error")
	}
	// map 截断：count=1 只有 key 无 value
	mbuf := wrapData(t, func() []byte {
		buf := hbuf.WriterType(nil, hbuf.TMap, 1, 1)
		buf = hbuf.WriterId(buf, 65)
		buf = hbuf.EncodeUint64(buf, 1)
		buf = hbuf.WriterType(buf, hbuf.TInt, 1, 1)
		return append(buf, 7)
	}(), 1)
	if err := hbuf.Unmarshal(mbuf, &des); err == nil {
		t.Errorf("truncated map should error")
	}
}

// ================= 14. map 边界 =================

func TestMapEdges(t *testing.T) {
	src := HBufTest{
		V66: map[int64]*int8{1: nil, 2: new(int8(0)), 3: new(int8(-1))}, // nil 值 / 零值 / 负值
		V84: map[int64]*string{0: new("")},                              // 空串指针值
		V97: map[string]int8{"": math.MinInt8},                          // 空 key
		V73: map[int64]uint8{0: 0},                                      // uint8 零值（元素 idLen=2）
	}
	buf, err := hbuf.Marshal(&src)
	if err != nil {
		t.Fatal(err)
	}
	var des HBufTest
	if err = hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	// *uint8 指向 0 与 nil 同样编码为空标记，解码统一回 nil（协议固有语义）
	if len(des.V66) != 3 || des.V66[1] != nil || des.V66[2] != nil || des.V66[3] == nil || *des.V66[3] != -1 {
		t.Errorf("V66 mismatch: %+v", des.V66)
	}
	// *string 指向 "" 同理解码回 nil
	if des.V84[0] != nil {
		t.Errorf("V84 mismatch: %+v", des.V84)
	}
	if des.V97[""] != math.MinInt8 {
		t.Errorf("V97 empty key mismatch: %v", des.V97)
	}
	if des.V73[0] != 0 {
		t.Errorf("V73 mismatch: %v", des.V73)
	}
}

// ================= 15. decimal 边界 =================

func TestDecimalEdges(t *testing.T) {
	cases := []string{
		"0",
		"0.0",
		"-0.0000000000000000000000000001",
		"123456789012345678901234567890.123456789",
		"1e+10",
		"1.5e-10",
		"99999999999999999999999999999999999999",
	}
	for _, s := range cases {
		src := HBufTest{V29: decimal.RequireFromString(s)}
		buf, err := hbuf.Marshal(&src)
		if err != nil {
			t.Errorf("%q marshal err=%v", s, err)
			continue
		}
		var des HBufTest
		if err = hbuf.Unmarshal(buf, &des); err != nil {
			t.Errorf("%q unmarshal err=%v", s, err)
			continue
		}
		if want := decimal.RequireFromString(s).String(); des.V29.String() != want {
			t.Errorf("decimal round-trip: got %q, want %q", des.V29.String(), want)
		}
	}
	// 列表中的 decimal 零值元素 → 空标记 → 解码为 decimal.Zero
	lst := HBufTest{V61: []decimal.Decimal{decimal.Zero, decimal.RequireFromString("1.5")}}
	buf, _ := hbuf.Marshal(&lst)
	var des HBufTest
	if err := hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	if len(des.V61) != 2 || !des.V61[0].IsZero() || des.V61[1].String() != "1.5" {
		t.Errorf("V61 mismatch: %v", des.V61)
	}
}

// ================= 16. 跨类型解码矩阵 =================

// crossInputs 为同一目标字段构造 5 种输入类型字节：
// TBool(true) / TInt(42) / TUint(43) / TFloat(3.5) / TBytes("44")
func crossInputs(t *testing.T, id uint16) [5][]byte {
	t.Helper()
	inputs := [5][]byte{}
	inputs[0] = func() []byte {
		buf := hbuf.WriterType(nil, hbuf.TBool, hbuf.LengthId(id), 2)
		return hbuf.WriterId(buf, id)
	}()
	inputs[1] = fieldInt(id, 42)
	inputs[2] = func() []byte {
		buf := hbuf.WriterType(nil, hbuf.TUint, hbuf.LengthId(id), 1)
		buf = hbuf.WriterId(buf, id)
		return hbuf.EncodeUint64(buf, 43)
	}()
	inputs[3] = func() []byte {
		bits := math.Float64bits(3.5)
		buf := hbuf.WriterType(nil, hbuf.TFloat, hbuf.LengthId(id), hbuf.LengthUint(bits))
		buf = hbuf.WriterId(buf, id)
		return hbuf.EncodeUint64(buf, bits)
	}()
	inputs[4] = fieldStr(id, "44")
	return inputs
}

func TestCrossTypeMatrix(t *testing.T) {
	matrix := []struct {
		name string
		id   uint16
		get  func(*HBufTest) string
		want [5]string
	}{
		{"int8", 1, func(d *HBufTest) string { return fmt.Sprint(d.V1) },
			[5]string{"1", "42", "43", "3", "44"}},
		{"int16", 3, func(d *HBufTest) string { return fmt.Sprint(d.V3) },
			[5]string{"1", "42", "43", "3", "44"}},
		{"int32", 5, func(d *HBufTest) string { return fmt.Sprint(d.V5) },
			[5]string{"1", "42", "43", "3", "44"}},
		{"int64", 7, func(d *HBufTest) string { return fmt.Sprint(d.V7) },
			[5]string{"1", "42", "43", "3", "44"}},
		{"uint8", 9, func(d *HBufTest) string { return fmt.Sprint(d.V9) },
			[5]string{"1", "42", "43", "3", "44"}},
		{"uint16", 11, func(d *HBufTest) string { return fmt.Sprint(d.V11) },
			[5]string{"1", "42", "43", "3", "44"}},
		{"uint32", 13, func(d *HBufTest) string { return fmt.Sprint(d.V13) },
			[5]string{"1", "42", "43", "3", "44"}},
		{"uint64", 15, func(d *HBufTest) string { return fmt.Sprint(d.V15) },
			[5]string{"1", "42", "43", "3", "44"}},
		{"float", 23, func(d *HBufTest) string { return fmt.Sprint(d.V23) },
			[5]string{"1", "42", "43", "3.5", "44"}},
		{"double", 25, func(d *HBufTest) string { return fmt.Sprint(d.V25) },
			[5]string{"1", "42", "43", "3.5", "44"}},
		{"bool", 17, func(d *HBufTest) string { return fmt.Sprint(d.V17) },
			[5]string{"true", "true", "true", "true", "false"}},
		{"string", 19, func(d *HBufTest) string { return fmt.Sprint(d.V19) },
			[5]string{"true", "42", "43", "3.5", "44"}},
		{"bytes", 21, func(d *HBufTest) string { return fmt.Sprint(d.V21) },
			[5]string{"[116 114 117 101]", "[52 50]", "[52 51]", "[51 46 53]", "[52 52]"}},
		{"decimal", 29, func(d *HBufTest) string { return fmt.Sprint(d.V29) },
			[5]string{"1", "42", "43", "3.5", "44"}},
	}
	typeNames := []string{"bool", "int", "uint", "float", "bytes"}
	for _, m := range matrix {
		inputs := crossInputs(t, m.id)
		for i := range inputs {
			buf := wrapData(t, inputs[i], 1)
			var des HBufTest
			if err := hbuf.Unmarshal(buf, &des); err != nil {
				t.Errorf("%s<-%s: err=%v", m.name, typeNames[i], err)
				continue
			}
			if got := m.get(&des); got != m.want[i] {
				t.Errorf("%s<-%s: got %s, want %s", m.name, typeNames[i], got, m.want[i])
			}
		}
	}
}

// ================= 17. 容器类型错误分支 =================

func TestInvalidContainerType(t *testing.T) {
	var des HBufTest
	// 顶层非 TData
	if err := hbuf.Unmarshal(fieldInt(1, 1), &des); err == nil {
		t.Errorf("top-level TInt should error")
	}
	// list 字段（V33 []int8）收到 TInt
	lbuf := wrapData(t, fieldInt(33, 1), 1)
	if err := hbuf.Unmarshal(lbuf, &des); err == nil || !strings.Contains(err.Error(), "invalid list type") {
		t.Errorf("list field <- TInt: err=%v, want invalid list type", err)
	}
	// map 字段（V65 map[int64]int8）收到 TInt
	mbuf := wrapData(t, fieldInt(65, 1), 1)
	if err := hbuf.Unmarshal(mbuf, &des); err == nil || !strings.Contains(err.Error(), "invalid map type") {
		t.Errorf("map field <- TInt: err=%v, want invalid map type", err)
	}
	// data 字段（V31 HBufSubTest）收到 TInt
	dbuf := wrapData(t, fieldInt(31, 1), 1)
	if err := hbuf.Unmarshal(dbuf, &des); err == nil || !strings.Contains(err.Error(), "invalid data type") {
		t.Errorf("data field <- TInt: err=%v, want invalid data type", err)
	}
	// string 字段（V19）收到 TMap
	sbuf := wrapData(t, func() []byte {
		b := hbuf.WriterType(nil, hbuf.TMap, 1, 1)
		b = hbuf.WriterId(b, 19)
		return hbuf.EncodeUint64(b, 0)
	}(), 1)
	if err := hbuf.Unmarshal(sbuf, &des); err == nil || !strings.Contains(err.Error(), "invalid string type") {
		t.Errorf("string field <- TMap: err=%v, want invalid string type", err)
	}
	// bytes 字段（V21）收到 TMap
	bbuf := wrapData(t, func() []byte {
		b := hbuf.WriterType(nil, hbuf.TMap, 1, 1)
		b = hbuf.WriterId(b, 21)
		return hbuf.EncodeUint64(b, 0)
	}(), 1)
	if err := hbuf.Unmarshal(bbuf, &des); err == nil || !strings.Contains(err.Error(), "invalid bytes type") {
		t.Errorf("bytes field <- TMap: err=%v, want invalid bytes type", err)
	}
}

// ================= 18. count==0 的容器解码 =================

func TestEmptyContainerDecode(t *testing.T) {
	// count==0 的 list/map 字段：解码提前返回，不重置原值
	pre := HBufTest{V33: []int8{1, 2}, V65: map[int64]int8{1: 1}}
	lbuf := wrapData(t, func() []byte {
		b := hbuf.WriterType(nil, hbuf.TList, 1, 1)
		b = hbuf.WriterId(b, 33)
		return hbuf.EncodeUint64(b, 0)
	}(), 1)
	mbuf := wrapData(t, func() []byte {
		b := hbuf.WriterType(nil, hbuf.TMap, 1, 1)
		b = hbuf.WriterId(b, 65)
		return hbuf.EncodeUint64(b, 0)
	}(), 1)
	// 两个字段一起：count=2
	fields := append([]byte{}, lbuf[2:len(lbuf)-1]...)
	fields = append(fields, mbuf[2:len(mbuf)-1]...)
	buf := wrapData(t, fields, 2)
	if err := hbuf.Unmarshal(buf, &pre); err != nil {
		t.Fatal(err)
	}
	if len(pre.V33) != 2 || pre.V33[0] != 1 || len(pre.V65) != 1 || pre.V65[1] != 1 {
		t.Errorf("count==0 container should not reset existing value: %+v", pre)
	}

	// 可空列表指针字段（BigListTest.V4）解码空列表 → 非 nil 空切片
	pbuf := wrapData(t, func() []byte {
		b := hbuf.WriterType(nil, hbuf.TList, 1, 1)
		b = hbuf.WriterId(b, 4)
		return hbuf.EncodeUint64(b, 0)
	}(), 1)
	var bd BigListTest
	if err := hbuf.Unmarshal(pbuf, &bd); err != nil {
		t.Fatal(err)
	}
	// count==0 提前返回，指针字段保持 nil（不分配）
	if bd.V4 != nil {
		t.Errorf("ptr list decode empty: got %v, want nil (count==0 not allocated)", bd.V4)
	}
	// 可空 map 字段：nil 省略；非 nil 空 map 写入 count=0；非 nil 非空 round-trip
	var msrc, mdes BigListTest
	buf1, _ := hbuf.Marshal(&msrc)
	if bytes.Contains(buf1, []byte{5}) {
		t.Errorf("nil map pointer should be omitted, buf=%x", buf1)
	}
	emptyMap := map[int64]int16{}
	msrc.V5 = &emptyMap
	buf2, _ := hbuf.Marshal(&msrc)
	if !bytes.Contains(buf2, []byte{0xC1, 5, 0}) { // TAG(TMap,1,1)=0xC1 | id=5 | count=0
		t.Errorf("non-nil empty map pointer should be written, buf=%x", buf2)
	}
	fullMap := map[int64]int16{1: 10, -2: 20}
	msrc.V5 = &fullMap
	buf3, err := hbuf.Marshal(&msrc)
	if err != nil {
		t.Fatal(err)
	}
	if err = hbuf.Unmarshal(buf3, &mdes); err != nil {
		t.Fatal(err)
	}
	if mdes.V5 == nil || len(*mdes.V5) != 2 || (*mdes.V5)[1] != 10 || (*mdes.V5)[-2] != 20 {
		t.Errorf("ptr map round-trip mismatch: %v", mdes.V5)
	}
}

// TestNestedEmptyElements 嵌套容器元素中的 nil / 空容器元素。
func TestNestedEmptyElements(t *testing.T) {
	// [][]byte：nil 元素与空切片元素均编码为空标记（idLen=0），解码统一回 nil
	// V64 []*HBufSubTest：nil 元素空标记 → nil；空结构元素 → 解码为 &HBufSubTest{}
	// V63 []HBufSubTest（值元素）：空结构写入 3 字节空 data → 解码回 {}
	src := HBufTest{
		V53: [][]byte{nil, {}, {1}},
		V63: []HBufSubTest{{}},
		V64: []*HBufSubTest{nil, {}},
	}
	buf, err := hbuf.Marshal(&src)
	if err != nil {
		t.Fatal(err)
	}
	var des HBufTest
	if err = hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	if len(des.V53) != 3 || des.V53[0] != nil || des.V53[1] != nil || !bytes.Equal(des.V53[2], []byte{1}) {
		t.Errorf("V53 nested empty mismatch: %v", des.V53)
	}
	if len(des.V63) != 1 || des.V63[0].A1 != 0 || des.V63[0].A2 != nil {
		t.Errorf("V63 value element mismatch: %+v", des.V63)
	}
	if len(des.V64) != 2 || des.V64[0] != nil || des.V64[1] == nil {
		t.Errorf("V64 nested data mismatch: %+v", des.V64)
	}
}

// ================= 19. 长字符串与长 bytes =================

func TestLongStringAndBytes(t *testing.T) {
	long := strings.Repeat("x", 1000)
	src := HBufTest{V19: long, V51: []string{long}, V21: bytes.Repeat([]byte{0xAB}, 1000)}
	buf, err := hbuf.Marshal(&src)
	if err != nil {
		t.Fatal(err)
	}
	var des HBufTest
	if err = hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	if des.V19 != long || len(des.V51) != 1 || des.V51[0] != long ||
		len(des.V21) != 1000 || !bytes.Equal(des.V21, bytes.Repeat([]byte{0xAB}, 1000)) {
		t.Errorf("long string/bytes round-trip mismatch")
	}
}
