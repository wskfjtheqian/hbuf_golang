package hbuf_test

import (
	"bytes"
	"encoding/json"
	hbuf "github.com/wskfjtheqian/hbuf_golang/pkg/buf"
	"google.golang.org/protobuf/proto"
	"reflect"
	"testing"
	"unsafe"
)

func TP[T any](v T) *T {
	return &v
}

var src = ProtoBuffTest{
	//V1: -0xFE,
	V2: TP[int64](122),
	V3: 88,
	////V4: 0xFF,
	////V5: -0xFF,
	////V6: -0xFF,
	V7:  []int64{0x01, 0x02, 0, 0x04, 0x05FF},
	V8:  map[int64]int64{0x01: 0x01, 0x02: 0x02, 0x03: 0x03, 0x04: 0x04, 0x05FF: 0x05FF},
	V9:  &ProtoBuffSub{V1: 55},
	V10: []*ProtoBuffSub{{V1: 0x01}, {V1: 0x02}, nil, {V1: 0x04}, {V1: 0x05FF}},
	V11: "hello world this is a test",
	V12: map[string]*ProtoBuffSub{"key1": {V1: 55}, "key2": {V1: 66}, "key3": nil, "key4": {V1: 88}, "key5": {V1: 99}},
}

var protoBuffSub ProtoBuffSub
var protoBuffSubDescriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(ProtoBuffSub{}), map[uint16]hbuf.Descriptor{
	1: hbuf.NewInt64Descriptor(unsafe.Offsetof(protoBuffSub.V1), false, "v"),
})

func (p *ProtoBuffSub) Descriptors() hbuf.Descriptor {
	return protoBuffSubDescriptor
}

var protoBuffTest ProtoBuffTest
var protoBuffTestDescriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(ProtoBuffTest{}), map[uint16]hbuf.Descriptor{
	2:  hbuf.NewInt64Descriptor(unsafe.Offsetof(protoBuffTest.V2), true, "v"),
	3:  hbuf.NewInt64Descriptor(unsafe.Offsetof(protoBuffTest.V3), false, "v"),
	7:  hbuf.NewListDescriptor[int64](unsafe.Offsetof(protoBuffTest.V7), hbuf.NewInt64Descriptor(0, false), "v"),
	8:  hbuf.NewMapDescriptor[int64, int64](unsafe.Offsetof(protoBuffTest.V8), hbuf.NewInt64Descriptor(0, false), hbuf.NewInt64Descriptor(0, false), "v"),
	9:  hbuf.CloneDataDescriptor(&ProtoBuffSub{}, unsafe.Offsetof(protoBuffTest.V9), true, "v"),
	10: hbuf.NewListDescriptor[*ProtoBuffSub](unsafe.Offsetof(protoBuffTest.V10), hbuf.CloneDataDescriptor(&ProtoBuffSub{}, 0, true), "v"),
	11: hbuf.NewStringDescriptor(unsafe.Offsetof(protoBuffTest.V11), false, "v"),
	12: hbuf.NewMapDescriptor[string, *ProtoBuffSub](unsafe.Offsetof(protoBuffTest.V12), hbuf.NewStringDescriptor(0, false), hbuf.CloneDataDescriptor(&ProtoBuffSub{}, 0, true), "v"),
})

func (x *ProtoBuffTest) Descriptors() hbuf.Descriptor {
	return protoBuffTestDescriptor
}

func TestName(t *testing.T) {
	var err error
	var pBuf []byte
	t.Run("EncoderProto", func(t *testing.T) {
		pBuf, err = proto.Marshal(&src)
		if err != nil {
			t.Error(err.Error())
			return
		}
		t.Log("len:", len(pBuf))
		t.Log("data:", pBuf)
	})
	t.Run("DecoderProto", func(t *testing.T) {
		des := ProtoBuffTest{}
		err = proto.Unmarshal(pBuf, &des)
		if err != nil {
			t.Error(err.Error() + "\n" + string(pBuf))
			return
		}
	})

	t.Run("EncoderJson", func(t *testing.T) {
		buf, err := json.Marshal(&src)
		if err != nil {
			t.Error(err.Error() + "\n" + string(buf))
			return
		}
		t.Log("len:", len(buf))
		t.Log("EncoderJson:", string(buf))
	})

	var hBuf []byte
	t.Run("EncoderHBuf", func(t *testing.T) {
		hBuf, err = hbuf.Marshal(&src, "v")
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
		t.Log("len:", len(hBuf))
		t.Log("EncoderHBuf:", hBuf)
	})
	t.Run("DecoderHBuf", func(t *testing.T) {
		des := ProtoBuffTest{}
		err := hbuf.Unmarshal(hBuf, &des)
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
		buf, err := json.Marshal(&des)
		t.Log("DecoderHBuf:", string(buf))

	})
	//	t.Log(src)
	//	if des.V3 != src.V3 {
	//		t.Error("V3 not equal")
	//	}
	//	if len(des.V7) != len(src.V7) {
	//		t.Error("V7 not equal")
	//	}
	//	for i, v := range des.V7 {
	//		if v != src.V7[i] {
	//			t.Error("V7 not equal")
	//		}
	//	}
	//	if len(des.V8) != len(src.V8) {
	//		t.Error("V8 not equal")
	//	}
	//	for k, v := range des.V8 {
	//		if v != src.V8[k] {
	//			t.Error("V8 not equal")
	//		}
	//	}
	//	if des.V9 != nil && src.V9 != nil && des.V9.V1 != src.V9.V1 {
	//		t.Error("V9 not equal")
	//	}
	//	if len(des.V10) != len(src.V10) {
	//		t.Error("V10 not equal")
	//	}
	//	for i, v := range des.V10 {
	//		if v.V1 != src.V10[i].V1 {
	//			t.Error("V10 not equal")
	//		}
	//	}
	//})
}

func BenchmarkName(b *testing.B) {
	var pBuf []byte
	var err error
	b.Run("EncoderProto", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			pBuf, err = proto.Marshal(&src)
			if err != nil {
				b.Error(err.Error() + "\n" + string(pBuf))
				return
			}
		}
	})
	b.Run("DecoderProto", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			des := ProtoBuffTest{}
			err := proto.Unmarshal(pBuf, &des)
			if err != nil {
				b.Error(err.Error() + "\n" + string(pBuf))
				return
			}
		}
	})

	var jBuf *bytes.Buffer
	b.Run("EncoderJson", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			jBuf = bytes.NewBuffer(nil)
			err := json.NewEncoder(jBuf).Encode(&src)
			if err != nil {
				b.Error(err.Error() + "\n" + jBuf.String())
				return
			}
		}
	})
	b.Run("DecoderJson", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			des := ProtoBuffTest{}
			err := json.NewDecoder(bytes.NewReader(jBuf.Bytes())).Decode(&des)
			if err != nil {
				b.Error(err.Error())
				return
			}

		}
	})

	var hBuf []byte
	b.Run("EncoderHBuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			hBuf, err = hbuf.Marshal(&src, "v")
			if err != nil {
				b.Error(err.Error() + "\n" + string(hBuf))
				return
			}
		}
	})
	b.Run("DecoderHBuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			des := ProtoBuffTest{}
			err = hbuf.Unmarshal(hBuf, &des)
			if err != nil {
				b.Error(err.Error())
				return
			}
		}
	})
}
