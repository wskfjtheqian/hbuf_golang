package hbuf_test

import (
	"github.com/shopspring/decimal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"reflect"
	"unsafe"
)

var hBufSubTest HBufSubTest
var hBufSubTestDescriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(&hBufSubTest), map[uint16]hbuf.Descriptor{
	1: hbuf.NewInt8Descriptor(unsafe.Offsetof(hBufSubTest.V1), false),
	2: hbuf.NewInt8Descriptor(unsafe.Offsetof(hBufSubTest.V2), true),
})

type HBufSubTest struct {
	V1 int8  `json:"v1,omitempty" hbuf:"1"` //
	V2 *int8 `json:"v2,omitempty" hbuf:"2"` //
}

func (g *HBufSubTest) Descriptors() hbuf.Descriptor {
	return hBufSubTestDescriptor
}

func (g *HBufSubTest) GetV1() int8 {
	return g.V1
}

func (g *HBufSubTest) SetV1(val int8) {
	g.V1 = val
}

func (g *HBufSubTest) GetV2() int8 {
	if nil == g.V2 {
		return int8(0)
	}
	return *g.V2
}

func (g *HBufSubTest) SetV2(val int8) {
	g.V2 = &val
}

var hBufTest HBufTest
var hBufTestDescriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(&hBufTest), map[uint16]hbuf.Descriptor{
	0:  hbuf.NewInt8Descriptor(unsafe.Offsetof(hBufTest.V1), false),
	1:  hbuf.NewInt8Descriptor(unsafe.Offsetof(hBufTest.V2), true),
	2:  hbuf.NewInt16Descriptor(unsafe.Offsetof(hBufTest.V3), false),
	3:  hbuf.NewInt16Descriptor(unsafe.Offsetof(hBufTest.V4), true),
	4:  hbuf.NewInt32Descriptor(unsafe.Offsetof(hBufTest.V5), false),
	5:  hbuf.NewInt32Descriptor(unsafe.Offsetof(hBufTest.V6), true),
	6:  hbuf.NewInt64Descriptor(unsafe.Offsetof(hBufTest.V7), false),
	7:  hbuf.NewInt64Descriptor(unsafe.Offsetof(hBufTest.V8), true),
	8:  hbuf.NewUint8Descriptor(unsafe.Offsetof(hBufTest.V9), false),
	9:  hbuf.NewUint8Descriptor(unsafe.Offsetof(hBufTest.V10), true),
	10: hbuf.NewUint16Descriptor(unsafe.Offsetof(hBufTest.V11), false),
	11: hbuf.NewUint16Descriptor(unsafe.Offsetof(hBufTest.V12), true),
	12: hbuf.NewUint32Descriptor(unsafe.Offsetof(hBufTest.V13), false),
	13: hbuf.NewUint32Descriptor(unsafe.Offsetof(hBufTest.V14), true),
	14: hbuf.NewUint64Descriptor(unsafe.Offsetof(hBufTest.V15), false),
	15: hbuf.NewUint64Descriptor(unsafe.Offsetof(hBufTest.V16), true),
	16: hbuf.NewBoolDescriptor(unsafe.Offsetof(hBufTest.V17), false),
	17: hbuf.NewBoolDescriptor(unsafe.Offsetof(hBufTest.V18), true),
	18: hbuf.NewStringDescriptor(unsafe.Offsetof(hBufTest.V19), false),
	19: hbuf.NewStringDescriptor(unsafe.Offsetof(hBufTest.V20), true),
	20: hbuf.NewBytesDescriptor(unsafe.Offsetof(hBufTest.V21), false),
	21: hbuf.NewBytesDescriptor(unsafe.Offsetof(hBufTest.V22), true),
	22: hbuf.NewFloatDescriptor(unsafe.Offsetof(hBufTest.V23), false),
	23: hbuf.NewFloatDescriptor(unsafe.Offsetof(hBufTest.V24), true),
	24: hbuf.NewDoubleDescriptor(unsafe.Offsetof(hBufTest.V25), false),
	25: hbuf.NewDoubleDescriptor(unsafe.Offsetof(hBufTest.V26), true),
	26: hbuf.NewTimeDescriptor(unsafe.Offsetof(hBufTest.V27), false),
	27: hbuf.NewTimeDescriptor(unsafe.Offsetof(hBufTest.V28), true),
	28: hbuf.NewDecimalDescriptor(unsafe.Offsetof(hBufTest.V29), false),
	29: hbuf.NewDecimalDescriptor(unsafe.Offsetof(hBufTest.V30), true),
	30: hbuf.CloneDataDescriptor(&HBufSubTest{}, unsafe.Offsetof(hBufTest.V31), false),
	31: hbuf.CloneDataDescriptor(&HBufSubTest{}, unsafe.Offsetof(hBufTest.V32), true),
})

type HBufTest struct {
	V31 HBufSubTest      `json:"v31,omitempty" hbuf:"30"` //
	V32 *HBufSubTest     `json:"v32,omitempty" hbuf:"31"` //
	V1  int8             `json:"v1,omitempty" hbuf:"0"`   //
	V2  *int8            `json:"v2,omitempty" hbuf:"1"`   //
	V3  int16            `json:"v3,omitempty" hbuf:"2"`   //
	V4  *int16           `json:"v4,omitempty" hbuf:"3"`   //
	V5  int32            `json:"v5,omitempty" hbuf:"4"`   //
	V6  *int32           `json:"v6,omitempty" hbuf:"5"`   //
	V7  hbuf.Int64       `json:"v7,omitempty" hbuf:"6"`   //
	V8  *hbuf.Int64      `json:"v8,omitempty" hbuf:"7"`   //
	V9  uint8            `json:"v9,omitempty" hbuf:"8"`   //
	V10 *uint8           `json:"v10,omitempty" hbuf:"9"`  //
	V11 uint16           `json:"v11,omitempty" hbuf:"10"` //
	V12 *uint16          `json:"v12,omitempty" hbuf:"11"` //
	V13 uint32           `json:"v13,omitempty" hbuf:"12"` //
	V14 *uint32          `json:"v14,omitempty" hbuf:"13"` //
	V15 hbuf.Uint64      `json:"v15,omitempty" hbuf:"14"` //
	V16 *hbuf.Uint64     `json:"v16,omitempty" hbuf:"15"` //
	V17 bool             `json:"v17,omitempty" hbuf:"16"` //
	V18 *bool            `json:"v18,omitempty" hbuf:"17"` //
	V19 string           `json:"v19,omitempty" hbuf:"18"` //
	V20 *string          `json:"v20,omitempty" hbuf:"19"` //
	V21 []byte           `json:"v21,omitempty" hbuf:"20"` //
	V22 *[]byte          `json:"v22,omitempty" hbuf:"21"` //
	V23 float32          `json:"v23,omitempty" hbuf:"22"` //
	V24 *float32         `json:"v24,omitempty" hbuf:"23"` //
	V25 float64          `json:"v25,omitempty" hbuf:"24"` //
	V26 *float64         `json:"v26,omitempty" hbuf:"25"` //
	V27 hbuf.Time        `json:"v27,omitempty" hbuf:"26"` //
	V28 *hbuf.Time       `json:"v28,omitempty" hbuf:"27"` //
	V29 decimal.Decimal  `json:"v29,omitempty" hbuf:"28"` //
	V30 *decimal.Decimal `json:"v30,omitempty" hbuf:"29"` //

}

func (g *HBufTest) Descriptors() hbuf.Descriptor {
	return hBufTestDescriptor
}

func (g *HBufTest) GetV1() int8 {
	return g.V1
}

func (g *HBufTest) SetV1(val int8) {
	g.V1 = val
}

func (g *HBufTest) GetV2() int8 {
	if nil == g.V2 {
		return int8(0)
	}
	return *g.V2
}

func (g *HBufTest) SetV2(val int8) {
	g.V2 = &val
}

func (g *HBufTest) GetV3() int16 {
	return g.V3
}

func (g *HBufTest) SetV3(val int16) {
	g.V3 = val
}

func (g *HBufTest) GetV4() int16 {
	if nil == g.V4 {
		return int16(0)
	}
	return *g.V4
}

func (g *HBufTest) SetV4(val int16) {
	g.V4 = &val
}

func (g *HBufTest) GetV5() int32 {
	return g.V5
}

func (g *HBufTest) SetV5(val int32) {
	g.V5 = val
}

func (g *HBufTest) GetV6() int32 {
	if nil == g.V6 {
		return int32(0)
	}
	return *g.V6
}

func (g *HBufTest) SetV6(val int32) {
	g.V6 = &val
}

func (g *HBufTest) GetV7() hbuf.Int64 {
	return g.V7
}

func (g *HBufTest) SetV7(val hbuf.Int64) {
	g.V7 = val
}

func (g *HBufTest) GetV8() hbuf.Int64 {
	if nil == g.V8 {
		return hbuf.Int64(0)
	}
	return *g.V8
}

func (g *HBufTest) SetV8(val hbuf.Int64) {
	g.V8 = &val
}

func (g *HBufTest) GetV9() uint8 {
	return g.V9
}

func (g *HBufTest) SetV9(val uint8) {
	g.V9 = val
}

func (g *HBufTest) GetV10() uint8 {
	if nil == g.V10 {
		return uint8(0)
	}
	return *g.V10
}

func (g *HBufTest) SetV10(val uint8) {
	g.V10 = &val
}

func (g *HBufTest) GetV11() uint16 {
	return g.V11
}

func (g *HBufTest) SetV11(val uint16) {
	g.V11 = val
}

func (g *HBufTest) GetV12() uint16 {
	if nil == g.V12 {
		return uint16(0)
	}
	return *g.V12
}

func (g *HBufTest) SetV12(val uint16) {
	g.V12 = &val
}

func (g *HBufTest) GetV13() uint32 {
	return g.V13
}

func (g *HBufTest) SetV13(val uint32) {
	g.V13 = val
}

func (g *HBufTest) GetV14() uint32 {
	if nil == g.V14 {
		return uint32(0)
	}
	return *g.V14
}

func (g *HBufTest) SetV14(val uint32) {
	g.V14 = &val
}

func (g *HBufTest) GetV15() hbuf.Uint64 {
	return g.V15
}

func (g *HBufTest) SetV15(val hbuf.Uint64) {
	g.V15 = val
}

func (g *HBufTest) GetV16() hbuf.Uint64 {
	if nil == g.V16 {
		return hbuf.Uint64(0)
	}
	return *g.V16
}

func (g *HBufTest) SetV16(val hbuf.Uint64) {
	g.V16 = &val
}

func (g *HBufTest) GetV17() bool {
	return g.V17
}

func (g *HBufTest) SetV17(val bool) {
	g.V17 = val
}

func (g *HBufTest) GetV18() bool {
	if nil == g.V18 {
		return false
	}
	return *g.V18
}

func (g *HBufTest) SetV18(val bool) {
	g.V18 = &val
}

func (g *HBufTest) GetV19() string {
	return g.V19
}

func (g *HBufTest) SetV19(val string) {
	g.V19 = val
}

func (g *HBufTest) GetV20() string {
	if nil == g.V20 {
		return ""
	}
	return *g.V20
}

func (g *HBufTest) SetV20(val string) {
	g.V20 = &val
}

func (g *HBufTest) GetV21() []byte {
	return g.V21
}

func (g *HBufTest) SetV21(val []byte) {
	g.V21 = val
}

func (g *HBufTest) GetV22() []byte {
	if nil == g.V22 {
		return nil
	}
	return *g.V22
}

func (g *HBufTest) SetV22(val []byte) {
	g.V22 = &val
}

func (g *HBufTest) GetV23() float32 {
	return g.V23
}

func (g *HBufTest) SetV23(val float32) {
	g.V23 = val
}

func (g *HBufTest) GetV24() float32 {
	if nil == g.V24 {
		return float32(0)
	}
	return *g.V24
}

func (g *HBufTest) SetV24(val float32) {
	g.V24 = &val
}

func (g *HBufTest) GetV25() float64 {
	return g.V25
}

func (g *HBufTest) SetV25(val float64) {
	g.V25 = val
}

func (g *HBufTest) GetV26() float64 {
	if nil == g.V26 {
		return float64(0)
	}
	return *g.V26
}

func (g *HBufTest) SetV26(val float64) {
	g.V26 = &val
}

func (g *HBufTest) GetV27() hbuf.Time {
	return g.V27
}

func (g *HBufTest) SetV27(val hbuf.Time) {
	g.V27 = val
}

func (g *HBufTest) GetV28() hbuf.Time {
	if nil == g.V28 {
		return hbuf.Time{}
	}
	return *g.V28
}

func (g *HBufTest) SetV28(val hbuf.Time) {
	g.V28 = &val
}

func (g *HBufTest) GetV29() decimal.Decimal {
	return g.V29
}

func (g *HBufTest) SetV29(val decimal.Decimal) {
	g.V29 = val
}

func (g *HBufTest) GetV30() decimal.Decimal {
	if nil == g.V30 {
		return decimal.Zero
	}
	return *g.V30
}

func (g *HBufTest) SetV30(val decimal.Decimal) {
	g.V30 = &val
}

func (g *HBufTest) GetV31() HBufSubTest {
	return g.V31
}

func (g *HBufTest) SetV31(val HBufSubTest) {
	g.V31 = val
}

func (g *HBufTest) GetV32() HBufSubTest {
	if nil == g.V32 {
		return HBufSubTest{}
	}
	return *g.V32
}

func (g *HBufTest) SetV32(val HBufSubTest) {
	g.V32 = &val
}
