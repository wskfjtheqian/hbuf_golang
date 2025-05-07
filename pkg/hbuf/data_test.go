package hbuf_test

import (
	"bytes"
	"encoding/json"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/protobuf/proto"
	"math/rand"
	"testing"
)

var TestSubDataFields = map[uint16]hbuf.Descriptor{
	11: hbuf.NewInt32Descriptor(func(d any) *int32 {
		return &d.(*TestSubData).V11
	}, func(d any, v int32) {
		d.(*TestSubData).V11 = v
	}),
}

type TestSubData struct {
	V11 int32 `json:"v1" hbuf:"11"`
}

func (t TestSubData) Descriptor() map[uint16]hbuf.Descriptor {
	return TestSubDataFields
}

var TestDataFields = map[uint16]hbuf.Descriptor{
	11: hbuf.NewInt8Descriptor(func(d any) *int8 {
		return &d.(*TestData).V11
	}, func(d any, v int8) {
		d.(*TestData).V11 = v
	}),
	12: hbuf.NewInt16Descriptor(func(d any) *int16 {
		return &d.(*TestData).V12
	}, func(d any, v int16) {
		d.(*TestData).V12 = v
	}),
	31: hbuf.NewListDescriptor[int8](func(d any) any {
		return d.(*TestData).V31
	}, func(d any, v any) {
		d.(*TestData).V31 = v.([]int8)
	}, hbuf.NewInt8Descriptor(func(d any) *int8 {
		temp := d.(int8)
		return &temp
	}, func(d any, v int8) {
		*d.(*int8) = int8(v)
	})),
	51: hbuf.NewMapDescriptor[string, int8](func(d any) any {
		return d.(*TestData).V51
	}, func(d any, v any) {
		d.(*TestData).V51 = v.(map[string]int8)
	}, hbuf.NewBytesDescriptor(func(d any) []byte {
		return []byte(d.(string))
	}, func(d any, v []byte) {
		*d.(*string) = string(v)
	}), hbuf.NewInt8Descriptor(func(d any) *int8 {
		temp := d.(int8)
		return &temp
	}, func(d any, v int8) {
		*d.(*int8) = int8(v)
	})),
	26: hbuf.NewDataDescriptor[*TestSubData](func(d any) *TestSubData {
		return &d.(*TestData).V26
	}, func(d any, v *TestSubData) {
		d.(*TestData).V26 = *v
	}),
	20: hbuf.NewBytesDescriptor(func(d any) []byte {
		return []byte(d.(*TestData).V20)
	}, func(d any, v []byte) {
		d.(*TestData).V20 = string(v)
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
	V18 uint64          `json:"v18,omitempty" hbuf:"18"`
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

func TestEncoderDecoder(t *testing.T) {
	p1 := TestStruct{
		ValueInt:     -2118888625,
		ValueInt8:    int32(int8(rand.Int63())),
		ValueInt16:   int32(int16(rand.Int63())),
		ValueInt32:   int32(rand.Int63()),
		ValueInt64:   int64(rand.Int63()),
		ValueUint:    uint32(uint(rand.Uint64())),
		ValueUint8:   uint32(uint8(rand.Uint64())),
		ValueUint16:  uint32(uint16(rand.Uint64())),
		ValueUint32:  uint32(rand.Uint64()),
		ValueUint64:  uint64(rand.Uint64()),
		ValueFloat32: float32(rand.Float32()),
		ValueFloat64: float64(rand.Float64()),
		ValueString:  "最佳答案：",
		ValueBytes:   []byte("length += 1"),
		ValueBool:    true,
		ValueData: &SubStruct{
			ValueInt:  123,
			ValueInt8: int32(int8(rand.Int63())),
		},
		ValueListInt: []int32{11, 22, 33, 44, 55},
		ValueListStr: []string{"11", "22", "uint16是无符号的16位整型数据类型", "44", "55"},
		ValueMapInt:  map[int32]int32{11: 111, 22: 222, 33: 333, 44: 444, 55: 555},
		ValueMapStr:  map[string]string{"11": "111", "22": "222", "33": "333", "44": "444", "55": "555"},
	}

	t1 := TestData{
		V11:       int8(rand.Int63()),
		V12:       int16(rand.Int63()),
		V13:       int32(rand.Int63()),
		V14:       int64(rand.Int63()),
		V15:       uint8(rand.Uint64()),
		V16:       uint16(rand.Uint64()),
		V17:       uint32(rand.Uint64()),
		V18:       uint64(rand.Uint64()),
		V24:       float32(rand.Float32()),
		V25:       float64(rand.Float64()),
		V18:       "最佳答案：",
		V19:       []byte("length += 1"),
		ValueBool: true,
		ValueData: subStruct{
			ValueInt:  123,
			ValueInt8: int8(rand.Int63()),
		},
		ValueListInt: []int{11, 22, 33, 44, 55},
		ValueListStr: []string{"11", "22", "uint16是无符号的16位整型数据类型", "44", "55"},
		ValueMapInt:  map[int]int{11: 111, 22: 222, 33: 333, 44: 444, 55: 555},
		ValueMapStr:  map[string]string{"11": "111", "22": "222", "33": "333", "44": "444", "55": "555"},
	}

	length := t1.Size()
	out := bytes.NewBuffer(make([]byte, 0, length))
	t.Run("Encoder", func(t *testing.T) {
		err := t1.Encoder(out)
		if err != nil {
			t.Error(err)
			return
		}
		t.Log(length)
		t.Log(out.Bytes())

		marshal, err := json.Marshal(&t1)
		if err != nil {
			return
		}
		t.Log(len(marshal))
		t.Log(string(marshal))

		data, err := proto.Marshal(&p1)
		if err != nil {
			return
		}
		t.Log(len(data))
	})

	t2 := TestData{}
	t.Run("Decoder", func(t *testing.T) {
		err := t2.Decoder(bytes.NewReader(out.Bytes()))
		if err != nil {
			t.Error(err)
			return
		}

	})

	if t1.ValueInt != t2.ValueInt {
		t.Error("not equal ValueInt", t1.ValueInt, t2.ValueInt)
	}
	if t1.ValueInt8 != t2.ValueInt8 {
		t.Error("not equal ValueInt8", t1.ValueInt8, t2.ValueInt8)
	}
	if t1.ValueInt16 != t2.ValueInt16 {
		t.Error("not equal ValueInt16", t1.ValueInt16, t2.ValueInt16)
	}
	if t1.ValueInt32 != t2.ValueInt32 {
		t.Error("not equal ValueInt32", t1.ValueInt32, t2.ValueInt32)
	}
	if t1.ValueInt64 != t2.ValueInt64 {
		t.Error("not equal ValueInt64", t1.ValueInt64, t2.ValueInt64)
	}
	if t1.ValueUint != t2.ValueUint {
		t.Error("not equal ValueUint", t1.ValueUint, t2.ValueUint)
	}
	if t1.ValueUint8 != t2.ValueUint8 {
		t.Error("not equal ValueUint8", t1.ValueUint8, t2.ValueUint8)
	}
	if t1.ValueUint16 != t2.ValueUint16 {
		t.Error("not equal ValueUint16", t1.ValueUint16, t2.ValueUint16)
	}
	if t1.ValueUint32 != t2.ValueUint32 {
		t.Error("not equal ValueUint32", t1.ValueUint32, t2.ValueUint32)
	}
	if t1.ValueUint64 != t2.ValueUint64 {
		t.Error("not equal ValueUint64", t1.ValueUint64, t2.ValueUint64)
	}
	if t1.ValueFloat32 != t2.ValueFloat32 {
		t.Error("not equal ValueFloat32", t1.ValueFloat32, t2.ValueFloat32)
	}
	if t1.ValueFloat64 != t2.ValueFloat64 {
		t.Error("not equal ValueFloat64", t1.ValueFloat64, t2.ValueFloat64)
	}
	if t1.ValueString != t2.ValueString {
		t.Error("not equal ValueString", t1.ValueString, t2.ValueString)
	}
	if bytes.Compare(t1.ValueBytes, t2.ValueBytes) != 0 {
		t.Error("not equal ValueBytes", t1.ValueBytes, t2.ValueBytes)
	}
	if t1.ValueBool != t2.ValueBool {
		t.Error("not equal ValueBool", t1.ValueBool, t2.ValueBool)
	}
	if t1.ValueData.ValueInt != t2.ValueData.ValueInt {
		t.Error("not equal ValueData.ValueInt", t1.ValueData.ValueInt, t2.ValueData.ValueInt)
	}
	if t1.ValueData.ValueInt8 != t2.ValueData.ValueInt8 {
		t.Error("not equal ValueData.ValueInt8", t1.ValueData.ValueInt8, t2.ValueData.ValueInt8)
	}
	if len(t1.ValueListInt) != len(t2.ValueListInt) {
		t.Error("not equal ValueListInt", t1.ValueListInt, t2.ValueListInt)
	}
	if len(t1.ValueListStr) != len(t2.ValueListStr) {
		t.Error("not equal ValueListStr", t1.ValueListStr, t2.ValueListStr)
	}
	for i := 0; i < len(t1.ValueListInt); i++ {
		if t1.ValueListInt[i] != t2.ValueListInt[i] {
			t.Error("not equal ValueListInt", t1.ValueListInt, t2.ValueListInt)
			break
		}
	}
	for i := 0; i < len(t1.ValueListStr); i++ {
		if t1.ValueListStr[i] != t2.ValueListStr[i] {
			t.Error("not equal ValueListStr", t1.ValueListStr, t2.ValueListStr)
			break
		}
	}
	for k, v := range t1.ValueMapInt {
		if t2.ValueMapInt[k] != v {
			t.Error("not equal ValueMapInt", t1.ValueMapInt, t2.ValueMapInt)
			break
		}
	}
	for k, v := range t1.ValueMapStr {
		if t2.ValueMapStr[k] != v {
			t.Error("not equal ValueMapStr", t1.ValueMapStr, t2.ValueMapStr)
			break
		}
	}
}

func BenchmarkEncoder(b *testing.B) {
	p1 := TestStruct{
		ValueInt:     0,
		ValueInt8:    int32(int8(rand.Int63())),
		ValueInt16:   int32(int16(rand.Int63())),
		ValueInt32:   int32(rand.Int63()),
		ValueInt64:   int64(rand.Int63()),
		ValueUint:    uint32(uint(rand.Uint64())),
		ValueUint8:   uint32(uint8(rand.Uint64())),
		ValueUint16:  uint32(uint16(rand.Uint64())),
		ValueUint32:  uint32(rand.Uint64()),
		ValueUint64:  uint64(rand.Uint64()),
		ValueFloat32: float32(rand.Float32()),
		ValueFloat64: float64(rand.Float64()),
		ValueString:  "最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案",
		ValueBytes:   []byte("ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:"),
		ValueBool:    true,
		ValueData: &SubStruct{
			ValueInt: 123,
		},
		ValueListInt: []int32{11, 22, 33, 44, 55},
		ValueListStr: []string{"11", "22", "uint16是无符号的16位整型数据类型", "44", "55"},
		ValueMapInt:  map[int32]int32{11: 111, 22: 222, 33: 333, 44: 444, 55: 555},
		ValueMapStr:  map[string]string{"11": "111", "22": "222", "33": "333", "44": "444", "55": "555"},
	}

	t1 := testStruct{
		ValueInt:     0,
		ValueInt8:    int8(rand.Int63()),
		ValueInt16:   int16(rand.Int63()),
		ValueInt32:   int32(rand.Int63()),
		ValueInt64:   int64(rand.Int63()),
		ValueUint:    uint(rand.Uint64()),
		ValueUint8:   uint8(rand.Uint64()),
		ValueUint16:  uint16(rand.Uint64()),
		ValueUint32:  uint32(rand.Uint64()),
		ValueUint64:  uint64(rand.Uint64()),
		ValueFloat32: float32(rand.Float32()),
		ValueFloat64: float64(rand.Float64()),
		ValueString:  "最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案最佳答案",
		ValueBytes:   []byte("ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:ValueFloat64:"),
		ValueBool:    true,
		ValueData: subStruct{
			ValueInt: 123,
		},
		ValueListInt: []int{11, 22, 33, 44, 55},
		ValueListStr: []string{"11", "22", "uint16是无符号的16位整型数据类型", "44", "55"},
		ValueMapInt:  map[int]int{11: 111, 22: 222, 33: 333, 44: 444, 55: 555},
		ValueMapStr:  map[string]string{"11": "111", "22": "222", "33": "333", "44": "444", "55": "555"},
	}
	b.Run("Encoder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			out := bytes.NewBuffer(make([]byte, 0, t1.Size()))
			err := t1.Encoder(out)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})
	b.Run("EncoderJSON", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			out := bytes.NewBuffer(nil)
			err := json.NewEncoder(out).Encode(t1)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})
	b.Run("EncoderProto", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := proto.Marshal(&p1)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})

	out := bytes.NewBuffer(make([]byte, 0, t1.Size()))
	err := t1.Encoder(out)
	if err != nil {
		b.Error(err)
		return
	}
	b.Run("DecoderHbuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t2 := testStruct{}
			err := t2.Decoder(bytes.NewReader(out.Bytes()))
			if err != nil {
				b.Error(err)
				return
			}
		}
	})

	out = bytes.NewBuffer(nil)
	err = json.NewEncoder(out).Encode(t1)
	if err != nil {
		b.Error(err)
		return
	}
	b.Run("DecoderHbufJSON", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t2 := testStruct{}
			err := json.NewDecoder(bytes.NewReader(out.Bytes())).Decode(&t2)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})

	data, err := proto.Marshal(&p1)
	if err != nil {
		b.Error(err)
		return
	}
	b.Run("DecoderHbufProto", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t2 := TestStruct{}
			err := proto.Unmarshal(data, &t2)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})
}
