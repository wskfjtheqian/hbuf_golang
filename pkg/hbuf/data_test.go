package hbuf_test

import (
	"bytes"
	"encoding/json"
	"github.com/shopspring/decimal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"google.golang.org/protobuf/proto"
	"math/rand"
	"testing"
	"time"
)

var p = ProtoBuffResp{
	V1:  rand.Int31(),
	V2:  rand.Int31(),
	V3:  rand.Int31(),
	V4:  rand.Int63(),
	V5:  rand.Uint32(),
	V6:  rand.Uint64(),
	V7:  rand.Uint32(),
	V8:  rand.Uint64(),
	V9:  false,
	V10: rand.Float32(),
	V11: rand.Float64(),
	V12: "option go_package = \"github.com/chenmingyong0423/blog/tutorial-code/go/protobuf/proto/user\"",
	V13: rand.Uint32(),
	V14: []byte("option go_package = \"github.com/chenmingyong0423/blog/tutorial-code/go/protobuf/proto/user\""),
	V16: &ProtoBuffReq{
		UserId: rand.Int63(),
		Name:   "option go_package = \"github.com/chenmingyong0423/blog/tutorial-code/go/protobuf/proto/user\"",
		Age:    rand.Int31(),
	},
	//V17: []int32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V18: []int32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V19: []int32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V20: []int64{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V21: []uint32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V22: []uint32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V23: []uint32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V24: []uint64{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V25: []bool{false, true, false, true, false, true, false, false},
	//V26: []float32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V27: []float64{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V28: []string{"11", "12", "13", "14", "15", "16", "17", "18", "19", "20"},
	//V29: []uint32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V30: [][]byte{[]byte("11"), []byte("12"), []byte("13"), []byte("14"), []byte("15"), []byte("16")},
	//V32: []*ProtoBuffReq{{UserId: 11, Name: "111", Age: 111}, {UserId: 22, Name: "222", Age: 222}, {UserId: 33, Name: "333", Age: 333}, {UserId: 44, Name: "444", Age: 444}, {UserId: 55, Name: "555", Age: 555}},
}

var d = GetInfoResp{
	V1:  int8(rand.Int31()),
	V2:  int16(rand.Int31()),
	V3:  rand.Int31(),
	V4:  hbuf.Int64(rand.Int63()),
	V5:  uint8(rand.Uint32()),
	V6:  uint16(rand.Uint64()),
	V7:  rand.Uint32(),
	V8:  hbuf.Uint64(rand.Uint64()),
	V9:  false,
	V10: rand.Float32(),
	V11: rand.Float64(),
	V12: "option go_package = \"github.com/chenmingyong0423/blog/tutorial-code/go/protobuf/proto/user\"",
	V13: hbuf.Time(time.Now()),
	V14: decimal.NewFromInt(rand.Int63()),
	//V16: GetInfoReq{
	//	UserId: hbuf.Int64(rand.Int63()),
	//	Name:   "option go_package = \"github.com/chenmingyong0423/blog/tutorial-code/go/protobuf/proto/user\"",
	//	Age:    rand.Int31(),
	//},
	//V17: []int32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V18: []int32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V19: []int32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V20: []int64{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V21: []uint32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V22: []uint32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V23: []uint32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V24: []uint64{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V25: []bool{false, true, false, true, false, true, false, false},
	//V26: []float32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V27: []float64{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V28: []string{"11", "12", "13", "14", "15", "16", "17", "18", "19", "20"},
	//V29: []uint32{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	//V30: [][]byte{[]byte("11"), []byte("12"), []byte("13"), []byte("14"), []byte("15"), []byte("16")},
	//V32: []*ProtoBuffReq{{UserId: 11, Name: "111", Age: 111}, {UserId: 22, Name: "222", Age: 222}, {UserId: 33, Name: "333", Age: 333}, {UserId: 44, Name: "444", Age: 444}, {UserId: 55, Name: "555", Age: 555}},
}

func Test_EncodeData(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	err := hbuf.NewEncoder(buf).Encode(&d)
	if err != nil {
		t.Error(err)
		return
	}

	resp := GetInfoResp{}
	err = hbuf.NewDecoder(buf).Decode(&resp)
	if err != nil {
		t.Error(err)
		return
	}
	return
}

func Benchmark_EncodeData(b *testing.B) {

	b.Run("Benchmark_EncodeProtoBuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := proto.Marshal(&p)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})

	b.Run("Benchmark_EncodeJson", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := bytes.NewBuffer(nil)
			err := json.NewEncoder(buf).Encode(&p)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})

	b.Run("Benchmark_EncodeHbuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := bytes.NewBuffer(nil)
			err := hbuf.NewEncoder(buf).Encode(&d)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})
	buffer, err := proto.Marshal(&p)
	if err != nil {
		b.Error(err)
		return
	}
	b.Run("Benchmark_DecodeProtoBuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := ProtoBuffReq{}
			err = proto.Unmarshal(buffer, &s)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})

	buf := bytes.NewBuffer(nil)
	err = json.NewEncoder(buf).Encode(&p)
	if err != nil {
		b.Error(err)
		return
	}
	b.Run("Benchmark_DecodeJson", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := ProtoBuffReq{}
			err = json.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&s)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})

	buf = bytes.NewBuffer(nil)
	err = hbuf.NewEncoder(buf).Encode(&d)
	if err != nil {
		b.Error(err)
		return
	}
	b.Run("Benchmark_DecodeHbuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := GetInfoResp{}
			err = hbuf.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&s)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})
}
