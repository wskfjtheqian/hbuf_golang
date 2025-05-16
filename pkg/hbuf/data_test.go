package hbuf_test

import (
	"encoding/json"
	"github.com/shopspring/decimal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TP[T any](v T) *T {
	return &v
}

var src = HBufTest{
	V1:  12,
	V2:  TP(int8(12)),
	V3:  1234,
	V4:  TP(int16(1234)),
	V5:  123456,
	V6:  TP(int32(123456)),
	V7:  123456789,
	V8:  TP(hbuf.Int64(123456789)),
	V9:  200,
	V10: TP(uint8(200)),
	V11: 65530,
	V12: TP(uint16(65530)),
	V13: 655306553,
	V14: TP(uint32(655306553)),
	V15: 6553065535,
	V16: TP(hbuf.Uint64(6553065535)),
	V17: true,
	V18: TP(true),
	V19: "hello world",
	V20: TP("hello world"),
	V21: []byte("hello world123456"),
	V22: TP([]byte("hello world123456")),
	V23: 3.1415926535895781,
	V24: TP(float32(3.1415926535895781)),
	V25: 3.1415926535895781,
	V26: TP(float64(3.1415926535895781)),
	V27: hbuf.Time(time.Now()),
	V28: TP(hbuf.Time(time.Now())),
	V29: decimal.NewFromFloat(3.1415926535),
	V30: TP(decimal.NewFromFloat(3.14159265359)),
	V31: HBufSubTest{
		V1: 12,
		V2: TP(int8(12)),
	},
	V32: &HBufSubTest{
		V1: 45,
		V2: TP(int8(45)),
	},
}

func TestName(t *testing.T) {
	var err error
	//var pBuf []byte
	//t.Run("EncoderProto", func(t *testing.T) {
	//	pBuf, err = proto.Marshal(&src)
	//	if err != nil {
	//		t.Error(err.Error())
	//		return
	//	}
	//	t.Log("len:", len(pBuf))
	//	t.Log("data:", pBuf)
	//})
	//t.Run("DecoderProto", func(t *testing.T) {
	//	des := ProtoBuffTest{}
	//	err = proto.Unmarshal(pBuf, &des)
	//	if err != nil {
	//		t.Error(err.Error() + "\n" + string(pBuf))
	//		return
	//	}
	//})

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
		hBuf, err = hbuf.Marshal(&src, "")
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
		t.Log("len:", len(hBuf))
		t.Log("EncoderHBuf:", hBuf)
		err = os.WriteFile(filepath.Join(os.TempDir(), "test.bin"), hBuf, 0666)
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
	})
	t.Run("DecoderHBuf", func(t *testing.T) {
		hBuf, err = os.ReadFile(filepath.Join(os.TempDir(), "test.bin"))
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
		des := HBufTest{}
		err = hbuf.Unmarshal(hBuf, &des, "")
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
		buf, _ := json.Marshal(&des)
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
	//var err error
	//var pBuf []byte
	//b.Run("EncoderProto", func(b *testing.B) {
	//	for i := 0; i < b.N; i++ {
	//		pBuf, err = proto.Marshal(&src)
	//		if err != nil {
	//			b.Error(err.Error() + "\n" + string(pBuf))
	//			return
	//		}
	//	}
	//})
	//b.Run("DecoderProto", func(b *testing.B) {
	//	for i := 0; i < b.N; i++ {
	//		des := ProtoBuffTest{}
	//		err := proto.Unmarshal(pBuf, &des)
	//		if err != nil {
	//			b.Error(err.Error() + "\n" + string(pBuf))
	//			return
	//		}
	//	}
	//})

	//var jBuf *bytes.Buffer
	//b.Run("EncoderJson", func(b *testing.B) {
	//	for i := 0; i < b.N; i++ {
	//		jBuf = bytes.NewBuffer(nil)
	//		err := json.NewEncoder(jBuf).Encode(&src)
	//		if err != nil {
	//			b.Error(err.Error() + "\n" + jBuf.String())
	//			return
	//		}
	//	}
	//})
	//b.Run("DecoderJson", func(b *testing.B) {
	//	for i := 0; i < b.N; i++ {
	//		des := ProtoBuffTest{}
	//		err := json.NewDecoder(bytes.NewReader(jBuf.Bytes())).Decode(&des)
	//		if err != nil {
	//			b.Error(err.Error())
	//			return
	//		}
	//
	//	}
	//})
	//
	//var hBuf []byte
	//b.Run("EncoderHBuf", func(b *testing.B) {
	//	for i := 0; i < b.N; i++ {
	//		hBuf, err = hbuf.Marshal(&src, "v")
	//		if err != nil {
	//			b.Error(err.Error() + "\n" + string(hBuf))
	//			return
	//		}
	//	}
	//})
	//b.Run("DecoderHBuf", func(b *testing.B) {
	//	for i := 0; i < b.N; i++ {
	//		des := ProtoBuffTest{}
	//		err = hbuf.Unmarshal(hBuf, &des, "v")
	//		if err != nil {
	//			b.Error(err.Error())
	//			return
	//		}
	//	}
	//})
}
