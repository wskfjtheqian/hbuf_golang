package hbuf_test

import (
	"bytes"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/shopspring/decimal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
)

// ===================== POC：tag 反射构建 Descriptor（替代手写描述符表） =====================

func tagPtr[T any](v T) *T { return &v }

// newRichTagData 覆盖全部类型维度的富数据：标量/指针/值容器/指针容器/嵌套/映射两种键。
func newRichTagData() *HBufTest {
	tm := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	dec := decimal.RequireFromString("12345.6789")
	return &HBufTest{
		V1: -8, V2: tagPtr(int8(-16)), V3: -300, V4: tagPtr(int16(-600)),
		V5: -70000, V6: tagPtr(int32(-140000)), V7: -9000000000, V8: tagPtr(int64(-18000000000)),
		V9: 8, V10: tagPtr(uint8(16)), V11: 300, V12: tagPtr(uint16(600)),
		V13: 70000, V14: tagPtr(uint32(140000)), V15: 9000000000, V16: tagPtr(uint64(18000000000)),
		V17: true, V18: tagPtr(true), V19: "hello", V20: tagPtr("world"),
		V21: []byte{1, 2, 3}, V22: tagPtr([]byte{4, 5}),
		V23: 1.5, V24: tagPtr(float32(2.5)), V25: 3.5, V26: tagPtr(4.5),
		V27: tm, V28: &tm, V29: dec, V30: &dec,
		V31: HBufSubTest{A1: 1, A2: tagPtr(int64(2))},
		V32: &HBufSubTest{A1: 3},
		V33: []int8{1, -2}, V34: []*int8{tagPtr(int8(1)), nil},
		// V41 ([]uint8) 置空：Go 中 []uint8 与 []byte 同型，tag 反射无法区分；
		// 生成器对 list<uint8> 输出命名类型（见 TestTagNamedUint8List），此处避免歧义。
		V42: []*uint8{tagPtr(uint8(0))},
		V53: [][]byte{{1}, nil}, V54: []*[]byte{tagPtr([]byte{9})},
		V55: []float32{1.5}, V56: []*float32{tagPtr(float32(2.5))},
		V57: []float64{3.5}, V58: []*float64{tagPtr(4.5)},
		V59: []time.Time{tm}, V60: []*time.Time{&tm},
		V61: []decimal.Decimal{dec}, V62: []*decimal.Decimal{&dec},
		V63: []HBufSubTest{{A1: 5}}, V64: []*HBufSubTest{{A1: 6}, nil},
		V65: map[int64]int8{1: 2}, V66: map[int64]*int8{1: tagPtr(int8(0))},
		V83: map[int64]string{1: "a"}, V84: map[int64]*string{1: tagPtr("")},
		V85: map[int64][]byte{1: {1, 2}}, V86: map[int64]*[]byte{1: tagPtr([]byte{3})},
		V91: map[int64]time.Time{1: tm}, V92: map[int64]*time.Time{1: &tm},
		V93: map[int64]decimal.Decimal{1: dec}, V94: map[int64]*decimal.Decimal{1: &dec},
		V95: map[int64]HBufSubTest{1: {A1: 7}}, V96: map[int64]*HBufSubTest{1: {A1: 8}},
		V97: map[string]int8{"k": 1}, V98: map[string]*int8{"k": tagPtr(int8(2))},
		V115: map[string]string{"k": "v"}, V116: map[string]*string{"k": tagPtr("")},
		V117: map[string][]byte{"k": {1}}, V118: map[string]*[]byte{"k": tagPtr([]byte{2})},
		V123: map[string]time.Time{"k": tm}, V124: map[string]*time.Time{"k": &tm},
		V125: map[string]decimal.Decimal{"k": dec}, V126: map[string]*decimal.Decimal{"k": &dec},
		V127: map[string]HBufSubTest{"k": {A1: 9}}, V128: map[string]*HBufSubTest{"k": {A1: 10}},
	}
}

// TestTagDescriptorParity 手写描述符与 tag 反射构建描述符编码字节级一致。
func TestTagDescriptorParity(t *testing.T) {
	tagDesc := hbuf.ParseTypeDescriptor(reflect.TypeOf(HBufTest{}))
	cases := map[string]*HBufTest{
		"rich": newRichTagData(),
		"zero": {},
	}
	for name, src := range cases {
		handBuf, err := hbuf.Marshal(src)
		if err != nil {
			t.Fatalf("%s hand marshal: %v", name, err)
		}
		tagBuf := tagDesc.Encode(nil, unsafe.Pointer(src), nil)
		if !bytes.Equal(handBuf, tagBuf) {
			t.Errorf("%s: tag encode mismatch\n hand=%x\n tag =%x", name, handBuf, tagBuf)
		}
	}
}

// TestTagDescriptorRoundTrip tag 构建描述符解码 == 手写描述符解码（协议语义一致性）。
func TestTagDescriptorRoundTrip(t *testing.T) {
	tagDesc := hbuf.ParseTypeDescriptor(reflect.TypeOf(HBufTest{}))
	src := newRichTagData()
	buf, err := hbuf.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	var hand HBufTest
	if err = hbuf.Unmarshal(buf, &hand); err != nil {
		t.Fatal(err)
	}
	// tag 描述符解码同一字节流
	typ, _, valueLen, rest, err := hbuf.DecodeType(buf)
	if err != nil {
		t.Fatal(err)
	}
	var tag HBufTest
	if _, err = tagDesc.Decode(rest, unsafe.Pointer(&tag), typ, true, valueLen); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(hand, tag) {
		t.Errorf("tag decode mismatch hand vs tag:\n hand=%+v\n tag =%+v", hand, tag)
	}

	// 反向：tag 字节流用手写描述符解码
	tagBuf := tagDesc.Encode(nil, unsafe.Pointer(src), nil)
	var hand2 HBufTest
	if err = hbuf.Unmarshal(tagBuf, &hand2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(hand, hand2) {
		t.Errorf("hand decode of tag bytes mismatch")
	}
}

// ===================== 命名类型 list<uint8>（生成器约定） =====================

// TagUint8List 模拟生成器对 IDL list<uint8> 的 Go 映射：命名类型与 []byte 区分。
type TagUint8List []uint8

type TagUint8ListTest struct {
	V1 TagUint8List `hbuf:"1"`
}

func (g *TagUint8ListTest) Descriptors() hbuf.Descriptor {
	return hbuf.ParseTypeDescriptor(reflect.TypeOf(TagUint8ListTest{}))
}

// TestTagNamedUint8List 命名类型 []uint8 仍走 ListDescriptor（uint8 零值空标记特例）。
func TestTagNamedUint8List(t *testing.T) {
	src := &TagUint8ListTest{V1: TagUint8List{0, 1, 255}}
	buf, err := hbuf.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	// 与手写 ListDescriptor[uint8]（HBufTest.V41）的编码一致：
	// e101(根帧) | TAG(TList,1,1)=0xa1 | id=1 | count=3 | 0x22(元素0空标记) | 0x21 0x01 | 0x21 0xff | 00(EXTENDS)
	want := []byte{0xe1, 0x01, 0xa1, 1, 3, 0x22, 0x21, 0x01, 0x21, 0xff, 0x00}
	if !bytes.Equal(buf, want) {
		t.Errorf("named uint8 list encode = %x, want %x", buf, want)
	}
	var des TagUint8ListTest
	if err = hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}
	if len(des.V1) != 3 || des.V1[0] != 0 || des.V1[1] != 1 || des.V1[2] != 255 {
		t.Errorf("V1 mismatch: %v", des.V1)
	}
}

// ===================== 递归类型（tag 版） =====================
type TagDeepTest struct {
	V1 []*TagDeepTest `hbuf:"1"`
	V3 *TagDeepTest   `hbuf:"3"`
}

func (g *TagDeepTest) Descriptors() hbuf.Descriptor {
	return hbuf.ParseTypeDescriptor(reflect.TypeOf(TagDeepTest{}))
}

// TestTagDescriptorRecursive 递归类型的 tag 反射构建（RefDescriptor 延迟解析，不无限递归）。
func TestTagDescriptorRecursive(t *testing.T) {
	d3 := &TagDeepTest{}
	d2 := &TagDeepTest{V1: []*TagDeepTest{nil, d3, {}}}
	d1 := &TagDeepTest{V1: []*TagDeepTest{d2, nil}, V3: d3}

	buf, err := hbuf.Marshal(d1)
	if err != nil {
		t.Fatal(err)
	}
	var des TagDeepTest
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
}

// ===================== 性能对比 =====================

func BenchmarkHandMarshal(b *testing.B) {
	src := newRichTagData()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := hbuf.Marshal(src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTagMarshal(b *testing.B) {
	src := newRichTagData()
	tagDesc := hbuf.ParseTypeDescriptor(reflect.TypeOf(HBufTest{}))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = tagDesc.Encode(nil, unsafe.Pointer(src), nil)
	}
}

func BenchmarkHandUnmarshal(b *testing.B) {
	src := newRichTagData()
	buf, _ := hbuf.Marshal(src)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var des HBufTest
		if err := hbuf.Unmarshal(buf, &des); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTagUnmarshal(b *testing.B) {
	src := newRichTagData()
	buf, _ := hbuf.Marshal(src)
	tagDesc := hbuf.ParseTypeDescriptor(reflect.TypeOf(HBufTest{}))
	typ, _, valueLen, rest, _ := hbuf.DecodeType(buf)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var des HBufTest
		if _, err := tagDesc.Decode(rest, unsafe.Pointer(&des), typ, true, valueLen); err != nil {
			b.Fatal(err)
		}
	}
}
