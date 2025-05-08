package hbuf_test

import (
	"github.com/shopspring/decimal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"unsafe"
)

var GetInfoReqFields = map[uint16]hbuf.Descriptor{
	1: hbuf.NewInt64Descriptor(func(d unsafe.Pointer) *hbuf.Int64 {
		return &(*GetInfoReq)(d).UserId
	}, func(d unsafe.Pointer, v hbuf.Int64) {
		(*GetInfoReq)(d).UserId = v
	}),
	0: hbuf.NewStringDescriptor(func(d unsafe.Pointer) *string {
		return &(*GetInfoReq)(d).Name
	}, func(d unsafe.Pointer, v string) {
		(*GetInfoReq)(d).Name = v
	}),
	2: hbuf.NewInt32Descriptor(func(d unsafe.Pointer) *int32 {
		return &(*GetInfoReq)(d).Age
	}, func(d unsafe.Pointer, v int32) {
		(*GetInfoReq)(d).Age = v
	}),
}

type GetInfoReq struct {
	UserId hbuf.Int64 `json:"user_id,omitempty" hbuf:"1"` //
	Name   string     `json:"name,omitempty" hbuf:"0"`    //
	Age    int32      `json:"age,omitempty" hbuf:"2"`     //
}

func (g *GetInfoReq) Descriptor() map[uint16]hbuf.Descriptor {
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
	0: hbuf.NewInt8Descriptor(func(d unsafe.Pointer) *int8 {
		return &(*GetInfoResp)(d).V1
	}, func(d unsafe.Pointer, v int8) {
		(*GetInfoResp)(d).V1 = v
	}),
	1: hbuf.NewInt16Descriptor(func(d unsafe.Pointer) *int16 {
		return &(*GetInfoResp)(d).V2
	}, func(d unsafe.Pointer, v int16) {
		(*GetInfoResp)(d).V2 = v
	}),
	2: hbuf.NewInt32Descriptor(func(d unsafe.Pointer) *int32 {
		return &(*GetInfoResp)(d).V3
	}, func(d unsafe.Pointer, v int32) {
		(*GetInfoResp)(d).V3 = v
	}),
	3: hbuf.NewInt64Descriptor(func(d unsafe.Pointer) *hbuf.Int64 {
		return &(*GetInfoResp)(d).V4
	}, func(d unsafe.Pointer, v hbuf.Int64) {
		(*GetInfoResp)(d).V4 = v
	}),
	4: hbuf.NewUint8Descriptor(func(d unsafe.Pointer) *uint8 {
		return &(*GetInfoResp)(d).V5
	}, func(d unsafe.Pointer, v uint8) {
		(*GetInfoResp)(d).V5 = v
	}),
	5: hbuf.NewUint16Descriptor(func(d unsafe.Pointer) *uint16 {
		return &(*GetInfoResp)(d).V6
	}, func(d unsafe.Pointer, v uint16) {
		(*GetInfoResp)(d).V6 = v
	}),
	6: hbuf.NewUint32Descriptor(func(d unsafe.Pointer) *uint32 {
		return &(*GetInfoResp)(d).V7
	}, func(d unsafe.Pointer, v uint32) {
		(*GetInfoResp)(d).V7 = v
	}),
	7: hbuf.NewUint64Descriptor(func(d unsafe.Pointer) *hbuf.Uint64 {
		return &(*GetInfoResp)(d).V8
	}, func(d unsafe.Pointer, v hbuf.Uint64) {
		(*GetInfoResp)(d).V8 = v
	}),
	8: hbuf.NewBoolDescriptor(func(d unsafe.Pointer) *bool {
		return &(*GetInfoResp)(d).V9
	}, func(d unsafe.Pointer, v bool) {
		(*GetInfoResp)(d).V9 = v
	}),
	9: hbuf.NewFloatDescriptor(func(d unsafe.Pointer) *float32 {
		return &(*GetInfoResp)(d).V10
	}, func(d unsafe.Pointer, v float32) {
		(*GetInfoResp)(d).V10 = v
	}),
	10: hbuf.NewDoubleDescriptor(func(d unsafe.Pointer) *float64 {
		return &(*GetInfoResp)(d).V11
	}, func(d unsafe.Pointer, v float64) {
		(*GetInfoResp)(d).V11 = v
	}),
	11: hbuf.NewStringDescriptor(func(d unsafe.Pointer) *string {
		return &(*GetInfoResp)(d).V12
	}, func(d unsafe.Pointer, v string) {
		(*GetInfoResp)(d).V12 = v
	}),
	12: hbuf.NewTimeDescriptor(func(d unsafe.Pointer) *hbuf.Time {
		return &(*GetInfoResp)(d).V13
	}, func(d unsafe.Pointer, v hbuf.Time) {
		(*GetInfoResp)(d).V13 = v
	}),
	13: hbuf.NewDecimalDescriptor(func(d unsafe.Pointer) *decimal.Decimal {
		return &(*GetInfoResp)(d).V14
	}, func(d unsafe.Pointer, v decimal.Decimal) {
		(*GetInfoResp)(d).V14 = v
	}),
}

type GetInfoResp struct {
	V1  int8            `json:"v1,omitempty" hbuf:"0"`   //
	V2  int16           `json:"v2,omitempty" hbuf:"1"`   //
	V3  int32           `json:"v3,omitempty" hbuf:"2"`   //
	V4  hbuf.Int64      `json:"v4,omitempty" hbuf:"3"`   //
	V5  uint8           `json:"v5,omitempty" hbuf:"4"`   //
	V6  uint16          `json:"v6,omitempty" hbuf:"5"`   //
	V7  uint32          `json:"v7,omitempty" hbuf:"6"`   //
	V8  hbuf.Uint64     `json:"v8,omitempty" hbuf:"7"`   //
	V9  bool            `json:"v9,omitempty" hbuf:"8"`   //
	V10 float32         `json:"v10,omitempty" hbuf:"9"`  //
	V11 float64         `json:"v11,omitempty" hbuf:"10"` //
	V12 string          `json:"v12,omitempty" hbuf:"11"` //
	V13 hbuf.Time       `json:"v13,omitempty" hbuf:"12"` //
	V14 decimal.Decimal `json:"v14,omitempty" hbuf:"13"` //
	V16 GetInfoReq      `json:"v16,omitempty" hbuf:"15"` //
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

func (g *GetInfoResp) GetV16() GetInfoReq {
	return g.V16
}

func (g *GetInfoResp) SetV16(val GetInfoReq) {
	g.V16 = val
}
