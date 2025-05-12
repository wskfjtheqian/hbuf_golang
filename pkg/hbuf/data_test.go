package hbuf_test

import (
	"bytes"
	"encoding/json"
	hbuf "github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"google.golang.org/protobuf/proto"
	"testing"
)

var src = ProtoBuffTest{
	//V1: -0xFE,
	//V2: 0xFE,
	V3: 0xFF88,
	//V4: 0xFF,
	//V5: -0xFF,
	//V6: -0xFF,
	V7: []int64{0x01, 0x02, 1, 0x04, 0x05FF},
	V8: map[string]int64{"ad": 0x01, "ba": 0x02, "c": 0x03, "d": 0x04, "e": 0x05FF},
	V9: &ProtoBuffSub{
		V1: -0x055,
	},
	V10: []*ProtoBuffSub{{V1: 0x01}, {V1: 0x02}, {V1: 0x03}, {V1: 0x04}, {V1: 0x05FF}},
}

var protoBuffSubDescriptor = hbuf.NewDataDescriptor[*ProtoBuffSub](func(v any) *ProtoBuffSub {
	return v.(*ProtoBuffSub)
}, func(v any, value *ProtoBuffSub) {
	*(v.(**ProtoBuffSub)) = value
}, func() *ProtoBuffSub {
	return &ProtoBuffSub{}
}).AddField(1, hbuf.NewInt64Descriptor(func(v any) *int64 {
	return &v.(*ProtoBuffSub).V1
}, func(v any, value int64) {
	v.(*ProtoBuffSub).V1 = value
}))

func (p *ProtoBuffSub) Descriptors() hbuf.Descriptor {
	return protoBuffSubDescriptor
}

var protoBuffTestDescriptor = hbuf.NewDataDescriptor[*ProtoBuffTest](func(v any) *ProtoBuffTest {
	return v.(*ProtoBuffTest)
}, func(v any, value *ProtoBuffTest) {
	*(v.(**ProtoBuffTest)) = value
}, func() *ProtoBuffTest {
	return &ProtoBuffTest{}
}).AddField(3, hbuf.NewInt64Descriptor(func(v any) *int64 {
	return &v.(*ProtoBuffTest).V3
}, func(v any, value int64) {
	v.(*ProtoBuffTest).V3 = value
})).AddField(6, hbuf.NewInt64Descriptor(func(v any) *int64 {
	return &v.(*ProtoBuffTest).V6
}, func(v any, value int64) {
	v.(*ProtoBuffTest).V6 = value
})).AddField(7, hbuf.NewListDescriptor[int64](func(v any) []int64 {
	return v.(*ProtoBuffTest).V7
}, func(v any, value []int64) {
	v.(*ProtoBuffTest).V7 = value
}, hbuf.NewInt64Descriptor(func(v any) *int64 {
	val := v.(int64)
	return &val
}, func(v any, value int64) {
	*v.(*int64) = value
}))).AddField(8, hbuf.NewMapDescriptor[string, int64](func(v any) map[string]int64 {
	return v.(*ProtoBuffTest).V8
}, func(v any, value map[string]int64) {
	v.(*ProtoBuffTest).V8 = value
}, hbuf.NewStringDescriptor(func(v any) *string {
	val := v.(string)
	return &val
}, func(v any, value string) {
	*v.(*string) = value
}), hbuf.NewInt64Descriptor(func(v any) *int64 {
	val := v.(int64)
	return &val
}, func(v any, value int64) {
	*v.(*int64) = value
}))).AddField(9, hbuf.CloneDataDescriptor[*ProtoBuffSub](func(v any) *ProtoBuffSub {
	return v.(*ProtoBuffTest).V9
}, func(v any, value *ProtoBuffSub) {
	v.(*ProtoBuffTest).V9 = value
}, protoBuffSubDescriptor,
)).AddField(10, hbuf.NewListDescriptor[*ProtoBuffSub](func(v any) []*ProtoBuffSub {
	return v.(*ProtoBuffTest).V10
}, func(v any, value []*ProtoBuffSub) {
	v.(*ProtoBuffTest).V10 = value
}, protoBuffSubDescriptor))

func (x *ProtoBuffTest) Descriptors() hbuf.Descriptor {
	return protoBuffTestDescriptor
}

func TestName(t *testing.T) {
	var pBuf []byte
	var err error
	t.Run("EncoderProto", func(t *testing.T) {
		pBuf, err = proto.Marshal(&src)
		if err != nil {
			t.Error(err.Error() + "\n" + string(pBuf))
			return
		}
		t.Log("len:", len(pBuf))
	})
	t.Run("DecoderProto", func(t *testing.T) {
		des := ProtoBuffTest{}
		err := proto.Unmarshal(pBuf, &des)
		if err != nil {
			t.Error(err.Error() + "\n" + string(pBuf))
			return
		}
		t.Log(des)
	})
	t.Run("EncoderJson", func(t *testing.T) {
		buf, err := json.Marshal(&src)
		if err != nil {
			t.Error(err.Error() + "\n" + string(buf))
			return
		}
		t.Log("json:", string(buf))
	})

	hBuf := bytes.NewBuffer(nil)
	t.Run("EncoderHBuf", func(t *testing.T) {
		err := hbuf.NewEncoder(hBuf).Encode(&src)
		if err != nil {
			t.Error(err.Error() + "\n" + string(hBuf.String()))
			return
		}
		t.Log("len:", len(hBuf.String()))
	})
	t.Run("DecoderHBuf", func(t *testing.T) {
		des := ProtoBuffTest{}
		err := hbuf.NewDecoder(bytes.NewReader(hBuf.Bytes())).Decode(&des)
		if err != nil {
			return
		}
		t.Log(src)
		if des.V3 != src.V3 {
			t.Error("V3 not equal")
		}
		if len(des.V7) != len(src.V7) {
			t.Error("V7 not equal")
		}
		for i, v := range des.V7 {
			if v != src.V7[i] {
				t.Error("V7 not equal")
			}
		}
		if len(des.V8) != len(src.V8) {
			t.Error("V8 not equal")
		}
		for k, v := range des.V8 {
			if v != src.V8[k] {
				t.Error("V8 not equal")
			}
		}
		if des.V9 != nil && src.V9 != nil && des.V9.V1 != src.V9.V1 {
			t.Error("V9 not equal")
		}
		if len(des.V10) != len(src.V10) {
			t.Error("V10 not equal")
		}
		for i, v := range des.V10 {
			if v.V1 != src.V10[i].V1 {
				t.Error("V10 not equal")
			}
		}
	})
}

func BenchmarkName(b *testing.B) {
	var pBuf []byte
	b.Run("EncoderProto", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var err error
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

	var hBuf *bytes.Buffer
	b.Run("EncoderHBuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			hBuf = bytes.NewBuffer(nil)
			err := hbuf.NewEncoder(hBuf).Encode(&src)
			if err != nil {
				b.Error(err.Error() + "\n" + hBuf.String())
				return
			}
		}
	})
	b.Run("DecoderHBuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			des := ProtoBuffTest{}
			err := hbuf.NewDecoder(bytes.NewReader(hBuf.Bytes())).Decode(&des)
			if err != nil {
				b.Error(err.Error())
				return
			}
		}
	})
}
