package hbuf_test

import (
	"bytes"
	"encoding/json"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"google.golang.org/genproto/googleapis/type/decimal"
	"math/rand"
	"testing"
	"time"
)

var TestSubDataFields = map[uint16]hbuf.Descriptor{
	11: hbuf.NewInt64Descriptor(func(d any) int64 {
		return int64(d.(*TestSubData).V11)
	}, func(d any, v int64) {
		d.(*TestSubData).V11 = int32(v)
	}),
}

type TestSubData struct {
	V11 int32 `json:"v1" hbuf:"11"`
}

func (t TestSubData) Descriptor() map[uint16]hbuf.Descriptor {
	return TestSubDataFields
}

var TestDataFields = map[uint16]hbuf.Descriptor{
	11: hbuf.NewInt64Descriptor(func(d any) int64 {
		return int64(d.(*TestData).V11)
	}, func(d any, v int64) {
		d.(*TestData).V11 = int8(v)
	}),
	12: hbuf.NewInt64Descriptor(func(d any) int64 {
		return int64(d.(*TestData).V12)
	}, func(d any, v int64) {
		d.(*TestData).V12 = int16(v)
	}),
	13: hbuf.NewInt64Descriptor(func(d any) int64 {
		return int64(d.(*TestData).V13)
	}, func(d any, v int64) {
		d.(*TestData).V13 = int32(v)
	}),
	14: hbuf.NewInt64Descriptor(func(d any) int64 {
		return int64(d.(*TestData).V14)
	}, func(d any, v int64) {
		d.(*TestData).V14 = hbuf.Int64(v)
	}),
	15: hbuf.NewUint64Descriptor(func(d any) uint64 {
		return uint64(d.(*TestData).V15)
	}, func(d any, v uint64) {
		d.(*TestData).V15 = uint8(v)
	}),
	16: hbuf.NewUint64Descriptor(func(d any) uint64 {
		return uint64(d.(*TestData).V16)
	}, func(d any, v uint64) {
		d.(*TestData).V16 = uint16(v)
	}),
	17: hbuf.NewUint64Descriptor(func(d any) uint64 {
		return uint64(d.(*TestData).V17)
	}, func(d any, v uint64) {
		d.(*TestData).V17 = uint32(v)
	}),
	18: hbuf.NewUint64Descriptor(func(d any) uint64 {
		return uint64(d.(*TestData).V18)
	}, func(d any, v uint64) {
		d.(*TestData).V18 = hbuf.Uint64(v)
	}),
	19: hbuf.NewBytesDescriptor(func(d any) []byte {
		return d.(*TestData).V19
	}, func(d any, v []byte) {
		d.(*TestData).V19 = v
	}),
	20: hbuf.NewBytesDescriptor(func(d any) []byte {
		return []byte(d.(*TestData).V20)
	}, func(d any, v []byte) {
		d.(*TestData).V20 = string(v)
	}),
	21: hbuf.NewUint64Descriptor(func(d any) uint64 {
		return uint64(time.Time(d.(*TestData).V21).UnixMilli())
	}, func(d any, v uint64) {
		d.(*TestData).V21 = hbuf.Time(time.UnixMilli(int64(v)))
	}),
	31: hbuf.NewListDescriptor[int8](func(d any) any {
		return d.(*TestData).V31
	}, func(d any, v any) {
		d.(*TestData).V31 = v.([]int8)
	}, hbuf.NewInt64Descriptor(func(d any) int64 {
		return int64(d.(int8))
	}, func(d any, v int64) {
		*d.(*int8) = int8(v)
	})),
	40: hbuf.NewListDescriptor[string](func(d any) any {
		return d.(*TestData).V40
	}, func(d any, v any) {
		d.(*TestData).V40 = v.([]string)
	}, hbuf.NewBytesDescriptor(func(d any) []byte {
		return []byte(d.(string))
	}, func(d any, v []byte) {
		*d.(*string) = string(v)
	})),
	51: hbuf.NewMapDescriptor[string, int8](func(d any) any {
		return d.(*TestData).V51
	}, func(d any, v any) {
		d.(*TestData).V51 = v.(map[string]int8)
	}, hbuf.NewBytesDescriptor(func(d any) []byte {
		return []byte(d.(string))
	}, func(d any, v []byte) {
		*d.(*string) = string(v)
	}), hbuf.NewInt64Descriptor(func(d any) int64 {
		return int64(d.(int8))
	}, func(d any, v int64) {
		*d.(*int8) = int8(v)
	})),
	60: hbuf.NewMapDescriptor[string, string](func(d any) any {
		return d.(*TestData).V60
	}, func(d any, v any) {
		d.(*TestData).V60 = v.(map[string]string)
	}, hbuf.NewBytesDescriptor(func(d any) []byte {
		return []byte(d.(string))
	}, func(d any, v []byte) {
		*d.(*string) = string(v)
	}), hbuf.NewBytesDescriptor(func(d any) []byte {
		return []byte(d.(string))
	}, func(d any, v []byte) {
		*d.(*string) = string(v)
	})),
	26: hbuf.NewStructDescriptor(func(d any) any {
		return &d.(*TestData).V26
	}, func(d any, v any) {
		d.(*TestData).V26 = *v.(*TestSubData)
	}),
}

type TestData struct {
	V11 int8            `json:"v11,omitempty" hbuf:"11"`
	V12 int16           `json:"v12,omitempty" hbuf:"12"`
	V13 int32           `json:"v13,omitempty" hbuf:"13"`
	V14 hbuf.Int64      `json:"v14,omitempty" hbuf:"14"`
	V15 uint8           `json:"v15,omitempty" hbuf:"15"`
	V16 uint16          `json:"v16,omitempty" hbuf:"16"`
	V17 uint32          `json:"v17,omitempty" hbuf:"17"`
	V18 hbuf.Uint64     `json:"v18,omitempty" hbuf:"18"`
	V19 []byte          `json:"v19,omitempty" hbuf:"19"`
	V20 string          `json:"v20,omitempty" hbuf:"20"`
	V21 hbuf.Time       `json:"v21,omitempty" hbuf:"21"`
	V22 decimal.Decimal `json:"v22,omitempty" hbuf:"22"`
	V23 bool            `json:"v23,omitempty" hbuf:"23"`
	V24 float32         `json:"v24,omitempty" hbuf:"24"`
	V25 float64         `json:"v25,omitempty" hbuf:"25"`
	V26 TestSubData     `json:"v26,omitempty" hbuf:"26"`

	V31 []int8            `json:"v31,omitempty" hbuf:"31"`
	V32 []int16           `json:"v32,omitempty" hbuf:"32"`
	V33 []int32           `json:"v33,omitempty" hbuf:"33"`
	V34 []hbuf.Int64      `json:"v34,omitempty" hbuf:"34"`
	V35 []uint8           `json:"v35,omitempty" hbuf:"35"`
	V36 []uint16          `json:"v36,omitempty" hbuf:"36"`
	V37 []uint32          `json:"v37,omitempty" hbuf:"37"`
	V38 []uint64          `json:"v38,omitempty" hbuf:"38"`
	V39 [][]byte          `json:"v39,omitempty" hbuf:"39"`
	V40 []string          `json:"v40,omitempty" hbuf:"40"`
	V41 []hbuf.Time       `json:"v41,omitempty" hbuf:"41"`
	V42 []decimal.Decimal `json:"v42,omitempty" hbuf:"42"`
	V43 []bool            `json:"v43,omitempty" hbuf:"43"`
	V44 []float32         `json:"v44,omitempty" hbuf:"44"`
	V45 []float64         `json:"v45,omitempty" hbuf:"45"`

	V51 map[string]int8            `json:"v51,omitempty" hbuf:"51"`
	V52 map[string]int16           `json:"v52,omitempty" hbuf:"52"`
	V53 map[string]int32           `json:"v53,omitempty" hbuf:"53"`
	V54 map[string]int64           `json:"v54,omitempty" hbuf:"54"`
	V55 map[string]uint8           `json:"v55,omitempty" hbuf:"55"`
	V56 map[string]uint16          `json:"v56,omitempty" hbuf:"56"`
	V57 map[string]uint32          `json:"v57,omitempty" hbuf:"57"`
	V58 map[string]uint64          `json:"v58,omitempty" hbuf:"58"`
	V59 map[string][]byte          `json:"v59,omitempty" hbuf:"59"`
	V60 map[string]string          `json:"v60,omitempty" hbuf:"60"`
	V61 map[string]hbuf.Time       `json:"v61,omitempty" hbuf:"61"`
	V62 map[string]decimal.Decimal `json:"v62,omitempty" hbuf:"62"`
	V63 map[string]bool            `json:"v63,omitempty" hbuf:"63"`
	V64 map[string]float32         `json:"v64,omitempty" hbuf:"64"`
	V65 map[string]float64         `json:"v65,omitempty" hbuf:"65"`

	V71 map[int32]int8            `json:"v71,omitempty" hbuf:"71"`
	V72 map[int32]int16           `json:"v72,omitempty" hbuf:"72"`
	V73 map[int32]int32           `json:"v73,omitempty" hbuf:"73"`
	V74 map[int32]int64           `json:"v74,omitempty" hbuf:"74"`
	V75 map[int32]uint8           `json:"v75,omitempty" hbuf:"75"`
	V76 map[int32]uint16          `json:"v76,omitempty" hbuf:"76"`
	V77 map[int32]uint32          `json:"v77,omitempty" hbuf:"77"`
	V78 map[int32]uint64          `json:"v78,omitempty" hbuf:"78"`
	V79 map[int32][]byte          `json:"v79,omitempty" hbuf:"79"`
	V80 map[int32]string          `json:"v80,omitempty" hbuf:"80"`
	V81 map[int32]hbuf.Time       `json:"v81,omitempty" hbuf:"81"`
	V82 map[int32]decimal.Decimal `json:"v82,omitempty" hbuf:"82"`
	V83 map[int32]bool            `json:"v83,omitempty" hbuf:"83"`
	V84 map[int32]float32         `json:"v84,omitempty" hbuf:"84"`
	V85 map[int32]float64         `json:"v85,omitempty" hbuf:"85"`
}

func (t *TestData) Descriptor() map[uint16]hbuf.Descriptor {
	return TestDataFields
}

func Test_EncodeData(t *testing.T) {
	d := TestData{
		V11: int8(rand.Int63()),
		V12: int16(rand.Int63()),
		V13: int32(rand.Int63()),
		V14: hbuf.Int64(rand.Int63()),
		V15: uint8(rand.Uint64()),
		V16: uint16(rand.Uint64()),
		V17: uint32(rand.Uint64()),
		V18: hbuf.Uint64(rand.Uint64()),
		V19: []byte("ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:"),
		V20: "最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案",
		V21: hbuf.Time(time.Now()),
		V31: []int8{11, 22, 33, 44, 55},
		V40: []string{"11", "22", "uint16是无符号的16位整型数据类型", "44", "55"},
		V51: map[string]int8{"11": 111, "22": 22, "33": 33, "44": 44, "55": 55},
		V60: map[string]string{"11": "111", "22": "222", "33": "333", "44": "444", "55": "555"},
		V26: TestSubData{V11: 880011},
	}

	buf := bytes.NewBuffer(nil)
	err := hbuf.NewEncoder(buf).Encode(&d)
	if err != nil {
		t.Error(err)
		return
	}

	t.Log("buf size:", buf.Len())
	t.Log(buf.Bytes())

	marshal, err := json.Marshal(&d)
	if err != nil {
		return
	}
	t.Log("marshal size:", len(marshal))
	t.Log(string(marshal))

	s := TestData{}
	err = hbuf.NewDecoder(buf).Decode(&s)
	if err != nil {
		t.Error(err)
		return
	}

	if d.V11 != s.V11 {
		t.Errorf("got %d, want %d", d.V11, 11)
		return
	}
	if d.V12 != s.V12 {
		t.Errorf("got %d, want %d", d.V12, 12)
		return
	}

	if len(d.V31) != len(s.V31) {
		t.Error("list length not equal")
		return
	}

	for k, v := range d.V31 {
		if s.V31[k] != v {
			t.Error("list value not equal")
			return
		}
	}

	if len(d.V51) != len(s.V51) {
		t.Error("map length not equal")
		return
	}

	for k, v := range d.V51 {
		if s.V51[k] != v {
			t.Error("map value not equal")
			return
		}
	}
	if len(d.V62) != len(s.V62) {
		t.Error("map length not equal")
	}
	return
}

func Benchmark_EncodeData(b *testing.B) {
	d := TestData{
		V11: int8(rand.Int63()),
		V12: int16(rand.Int63()),
		V13: int32(rand.Int63()),
		V14: hbuf.Int64(rand.Int63()),
		V15: uint8(rand.Uint64()),
		V16: uint16(rand.Uint64()),
		V17: uint32(rand.Uint64()),
		V18: hbuf.Uint64(rand.Uint64()),
		V19: []byte("ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:"),
		V20: "最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案",
		V21: hbuf.Time(time.Now()),
		V31: []int8{11, 22, 33, 44, 55},
		V40: []string{"11", "22", "uint16是无符号的16位整型数据类型", "44", "55"},
		V51: map[string]int8{"11": 111, "22": 22, "33": 33, "44": 44, "55": 55},
		V60: map[string]string{"11": "111", "22": "222", "33": "333", "44": "444", "55": "555"},
		V26: TestSubData{V11: 880011},
	}

	b.Run("Benchmark_EncodeBuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := bytes.NewBuffer(nil)
			err := hbuf.NewEncoder(buf).Encode(&d)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})

	b.Run("Benchmark_EncodeJson", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := bytes.NewBuffer(nil)
			err := hbuf.NewEncoder(buf).Encode(&d)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})

	buf := bytes.NewBuffer(nil)
	err := hbuf.NewEncoder(buf).Encode(&d)
	if err != nil {
		b.Error(err)
		return
	}

	b.Run("Benchmark_DecodeBuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := TestData{}
			err = hbuf.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&s)
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
	b.Run("Benchmark_DecodeJson", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := TestData{}
			err = hbuf.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&s)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})
}
