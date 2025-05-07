package data_hbuf_test

import (
	"github.com/shopspring/decimal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
)

var GetInfoReqFields = map[uint16]hbuf.Descriptor[GetInfoReq]{
	1: hbuf.NewInt64Descriptor[GetInfoReq](func(d any) *hbuf.Int64 {
		return &d.(*GetInfoReq).UserId
	}, func(d any, v hbuf.Int64) {
		d.(*GetInfoReq).UserId = v
	}),
	0: hbuf.NewStringDescriptor(func(d any) *string {
		return &d.(*GetInfoReq).Name
	}, func(d any, v string) {
		d.(*GetInfoReq).Name = v
	}),
	2: hbuf.NewInt32Descriptor(func(d any) *int32 {
		return &d.(*GetInfoReq).Age
	}, func(d any, v int32) {
		d.(*GetInfoReq).Age = v
	}),
}

type GetInfoReq struct {
	UserId hbuf.Int64 `json:"user_id,omitempty" hbuf:"1"` //
	Name   string     `json:"name,omitempty" hbuf:"0"`    //
	Age    int32      `json:"age,omitempty" hbuf:"2"`     //
}

func (g *GetInfoReq) Descriptor() map[uint16]hbuf.Descriptor[GetInfoReq] {
	return GetInfoReqFields
}

func (g *GetInfoReq) GetUserId() hbuf.Int64 {
	return g.UserId
}

func (g *GetInfoReq) SetUserId(val hbuf.Int64) {
	g.UserId = val
}

func (g *GetInfoReq) GetName() string {
	return g.Name
}

func (g *GetInfoReq) SetName(val string) {
	g.Name = val
}

func (g *GetInfoReq) GetAge() int32 {
	return g.Age
}

func (g *GetInfoReq) SetAge(val int32) {
	g.Age = val
}

var GetInfoRespFields = map[uint16]hbuf.Descriptor{
	0: hbuf.NewInt8Descriptor(func(d any) *int8 {
		return &d.(*GetInfoResp).V1
	}, func(d any, v int8) {
		d.(*GetInfoResp).V1 = v
	}),
	1: hbuf.NewInt16Descriptor(func(d any) *int16 {
		return &d.(*GetInfoResp).V2
	}, func(d any, v int16) {
		d.(*GetInfoResp).V2 = v
	}),
	2: hbuf.NewInt32Descriptor(func(d any) *int32 {
		return &d.(*GetInfoResp).V3
	}, func(d any, v int32) {
		d.(*GetInfoResp).V3 = v
	}),
	3: hbuf.NewInt64Descriptor(func(d any) *hbuf.Int64 {
		return &d.(*GetInfoResp).V4
	}, func(d any, v hbuf.Int64) {
		d.(*GetInfoResp).V4 = v
	}),
	4: hbuf.NewUint8Descriptor(func(d any) *uint8 {
		return &d.(*GetInfoResp).V5
	}, func(d any, v uint8) {
		d.(*GetInfoResp).V5 = v
	}),
	5: hbuf.NewUint16Descriptor(func(d any) *uint16 {
		return &d.(*GetInfoResp).V6
	}, func(d any, v uint16) {
		d.(*GetInfoResp).V6 = v
	}),
	6: hbuf.NewUint32Descriptor(func(d any) *uint32 {
		return &d.(*GetInfoResp).V7
	}, func(d any, v uint32) {
		d.(*GetInfoResp).V7 = v
	}),
	7: hbuf.NewUint64Descriptor(func(d any) *hbuf.Uint64 {
		return &d.(*GetInfoResp).V8
	}, func(d any, v hbuf.Uint64) {
		d.(*GetInfoResp).V8 = v
	}),
	8: hbuf.NewBoolDescriptor(func(d any) *bool {
		return &d.(*GetInfoResp).V9
	}, func(d any, v bool) {
		d.(*GetInfoResp).V9 = v
	}),
	9: hbuf.NewFloatDescriptor(func(d any) *float32 {
		return &d.(*GetInfoResp).V10
	}, func(d any, v float32) {
		d.(*GetInfoResp).V10 = v
	}),
	10: hbuf.NewDoubleDescriptor(func(d any) *float64 {
		return &d.(*GetInfoResp).V11
	}, func(d any, v float64) {
		d.(*GetInfoResp).V11 = v
	}),
	11: hbuf.NewStringDescriptor(func(d any) *string {
		return &d.(*GetInfoResp).V12
	}, func(d any, v string) {
		d.(*GetInfoResp).V12 = v
	}),
	12: hbuf.NewTimeDescriptor(func(d any) *hbuf.Time {
		return &d.(*GetInfoResp).V13
	}, func(d any, v hbuf.Time) {
		d.(*GetInfoResp).V13 = v
	}),
	13: hbuf.NewDecimalDescriptor(func(d any) *decimal.Decimal {
		return &d.(*GetInfoResp).V14
	}, func(d any, v decimal.Decimal) {
		d.(*GetInfoResp).V14 = v
	}),
	14: hbuf.NewEnumDescriptor(func(d any) *Status {
		return &d.(*GetInfoResp).V15
	}, func(d any, v Status) {
		d.(*GetInfoResp).V15 = v
	}),
	15: hbuf.NewDataDescriptor(func(d any) *GetInfoReq {
		return &d.(*GetInfoResp).V16
	}, func(d any, v *GetInfoReq) {
		d.(*GetInfoResp).V16 = *v
	}),
	16: hbuf.NewListDescriptor[int8](func(d any) any {
		return &d.(*GetInfoResp).V17
	}, func(d any, v any) {
		d.(*GetInfoResp).V17 = v.([]int8)
	}, hbuf.NewInt8Descriptor(func(d any) *int8 {
		temp := d.(int8)
		return &temp
	}, func(d any, v int8) {
		*d.(*int8) = v
	})),
	17:,
	18:,
	19:,
	20:,
	21:,
	22:,
	23:,
	24:,
	25:,
	26:,
	27:,
	28:,
	29:,
	30:,
	31:,
}

type GetInfoResp struct {
	V1  int8              `json:"v1,omitempty" hbuf:"0"`   //
	V2  int16             `json:"v2,omitempty" hbuf:"1"`   //
	V3  int32             `json:"v3,omitempty" hbuf:"2"`   //
	V4  hbuf.Int64        `json:"v4,omitempty" hbuf:"3"`   //
	V5  uint8             `json:"v5,omitempty" hbuf:"4"`   //
	V6  uint16            `json:"v6,omitempty" hbuf:"5"`   //
	V7  uint32            `json:"v7,omitempty" hbuf:"6"`   //
	V8  hbuf.Uint64       `json:"v8,omitempty" hbuf:"7"`   //
	V9  bool              `json:"v9,omitempty" hbuf:"8"`   //
	V10 float32           `json:"v10,omitempty" hbuf:"9"`  //
	V11 float64           `json:"v11,omitempty" hbuf:"10"` //
	V12 string            `json:"v12,omitempty" hbuf:"11"` //
	V13 hbuf.Time         `json:"v13,omitempty" hbuf:"12"` //
	V14 decimal.Decimal   `json:"v14,omitempty" hbuf:"13"` //
	V15 Status            `json:"v15,omitempty" hbuf:"14"` //
	V16 GetInfoReq        `json:"v16,omitempty" hbuf:"15"` //
	V17 []int8            `json:"v17,omitempty" hbuf:"16"` //
	V18 []int16           `json:"v18,omitempty" hbuf:"17"` //
	V19 []int32           `json:"v19,omitempty" hbuf:"18"` //
	V20 []hbuf.Int64      `json:"v20,omitempty" hbuf:"19"` //
	V21 []uint8           `json:"v21,omitempty" hbuf:"20"` //
	V22 []uint16          `json:"v22,omitempty" hbuf:"21"` //
	V23 []uint32          `json:"v23,omitempty" hbuf:"22"` //
	V24 []hbuf.Uint64     `json:"v24,omitempty" hbuf:"23"` //
	V25 []bool            `json:"v25,omitempty" hbuf:"24"` //
	V26 []float32         `json:"v26,omitempty" hbuf:"25"` //
	V27 []float64         `json:"v27,omitempty" hbuf:"26"` //
	V28 []string          `json:"v28,omitempty" hbuf:"27"` //
	V29 []hbuf.Time       `json:"v29,omitempty" hbuf:"28"` //
	V30 []decimal.Decimal `json:"v30,omitempty" hbuf:"29"` //
	V31 []Status          `json:"v31,omitempty" hbuf:"30"` //
	V32 []GetInfoReq      `json:"v32,omitempty" hbuf:"31"` //
}

func (g *GetInfoResp) Descriptor() map[uint16]hbuf.Descriptor {
	return GetInfoRespFields
}

func (g *GetInfoResp) GetV1() int8 {
	return g.V1
}

func (g *GetInfoResp) SetV1(val int8) {
	g.V1 = val
}

func (g *GetInfoResp) GetV2() int16 {
	return g.V2
}

func (g *GetInfoResp) SetV2(val int16) {
	g.V2 = val
}

func (g *GetInfoResp) GetV3() int32 {
	return g.V3
}

func (g *GetInfoResp) SetV3(val int32) {
	g.V3 = val
}

func (g *GetInfoResp) GetV4() hbuf.Int64 {
	return g.V4
}

func (g *GetInfoResp) SetV4(val hbuf.Int64) {
	g.V4 = val
}

func (g *GetInfoResp) GetV5() uint8 {
	return g.V5
}

func (g *GetInfoResp) SetV5(val uint8) {
	g.V5 = val
}

func (g *GetInfoResp) GetV6() uint16 {
	return g.V6
}

func (g *GetInfoResp) SetV6(val uint16) {
	g.V6 = val
}

func (g *GetInfoResp) GetV7() uint32 {
	return g.V7
}

func (g *GetInfoResp) SetV7(val uint32) {
	g.V7 = val
}

func (g *GetInfoResp) GetV8() hbuf.Uint64 {
	return g.V8
}

func (g *GetInfoResp) SetV8(val hbuf.Uint64) {
	g.V8 = val
}

func (g *GetInfoResp) GetV9() bool {
	return g.V9
}

func (g *GetInfoResp) SetV9(val bool) {
	g.V9 = val
}

func (g *GetInfoResp) GetV10() float32 {
	return g.V10
}

func (g *GetInfoResp) SetV10(val float32) {
	g.V10 = val
}

func (g *GetInfoResp) GetV11() float64 {
	return g.V11
}

func (g *GetInfoResp) SetV11(val float64) {
	g.V11 = val
}

func (g *GetInfoResp) GetV12() string {
	return g.V12
}

func (g *GetInfoResp) SetV12(val string) {
	g.V12 = val
}

func (g *GetInfoResp) GetV13() hbuf.Time {
	return g.V13
}

func (g *GetInfoResp) SetV13(val hbuf.Time) {
	g.V13 = val
}

func (g *GetInfoResp) GetV14() decimal.Decimal {
	return g.V14
}

func (g *GetInfoResp) SetV14(val decimal.Decimal) {
	g.V14 = val
}

func (g *GetInfoResp) GetV15() Status {
	return g.V15
}

func (g *GetInfoResp) SetV15(val Status) {
	g.V15 = val
}

func (g *GetInfoResp) GetV16() GetInfoReq {
	return g.V16
}

func (g *GetInfoResp) SetV16(val GetInfoReq) {
	g.V16 = val
}

func (g *GetInfoResp) GetV17() []int8 {
	return g.V17
}

func (g *GetInfoResp) SetV17(val []int8) {
	g.V17 = val
}

func (g *GetInfoResp) GetV18() []int16 {
	return g.V18
}

func (g *GetInfoResp) SetV18(val []int16) {
	g.V18 = val
}

func (g *GetInfoResp) GetV19() []int32 {
	return g.V19
}

func (g *GetInfoResp) SetV19(val []int32) {
	g.V19 = val
}

func (g *GetInfoResp) GetV20() []hbuf.Int64 {
	return g.V20
}

func (g *GetInfoResp) SetV20(val []hbuf.Int64) {
	g.V20 = val
}

func (g *GetInfoResp) GetV21() []uint8 {
	return g.V21
}

func (g *GetInfoResp) SetV21(val []uint8) {
	g.V21 = val
}

func (g *GetInfoResp) GetV22() []uint16 {
	return g.V22
}

func (g *GetInfoResp) SetV22(val []uint16) {
	g.V22 = val
}

func (g *GetInfoResp) GetV23() []uint32 {
	return g.V23
}

func (g *GetInfoResp) SetV23(val []uint32) {
	g.V23 = val
}

func (g *GetInfoResp) GetV24() []hbuf.Uint64 {
	return g.V24
}

func (g *GetInfoResp) SetV24(val []hbuf.Uint64) {
	g.V24 = val
}

func (g *GetInfoResp) GetV25() []bool {
	return g.V25
}

func (g *GetInfoResp) SetV25(val []bool) {
	g.V25 = val
}

func (g *GetInfoResp) GetV26() []float32 {
	return g.V26
}

func (g *GetInfoResp) SetV26(val []float32) {
	g.V26 = val
}

func (g *GetInfoResp) GetV27() []float64 {
	return g.V27
}

func (g *GetInfoResp) SetV27(val []float64) {
	g.V27 = val
}

func (g *GetInfoResp) GetV28() []string {
	return g.V28
}

func (g *GetInfoResp) SetV28(val []string) {
	g.V28 = val
}

func (g *GetInfoResp) GetV29() []hbuf.Time {
	return g.V29
}

func (g *GetInfoResp) SetV29(val []hbuf.Time) {
	g.V29 = val
}

func (g *GetInfoResp) GetV30() []decimal.Decimal {
	return g.V30
}

func (g *GetInfoResp) SetV30(val []decimal.Decimal) {
	g.V30 = val
}

func (g *GetInfoResp) GetV31() []Status {
	return g.V31
}

func (g *GetInfoResp) SetV31(val []Status) {
	g.V31 = val
}

func (g *GetInfoResp) GetV32() []GetInfoReq {
	return g.V32
}

func (g *GetInfoResp) SetV32(val []GetInfoReq) {
	g.V32 = val
}
