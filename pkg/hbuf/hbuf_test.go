package hbuf_test

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"reflect"
	"unsafe"
)

var protoBufSubTest ProtoBufSubTest
var protoBufSubTestDescriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(&protoBufSubTest), map[uint16]hbuf.Descriptor{
	1: hbuf.NewInt8Descriptor(unsafe.Offsetof(protoBufSubTest.V1), false),
	2: hbuf.NewInt8Descriptor(unsafe.Offsetof(protoBufSubTest.V2), true),
})

type ProtoBufSubTest struct {
	V1 int8  `json:"v1,omitempty" hbuf:"1"` //
	V2 *int8 `json:"v2,omitempty" hbuf:"2"` //
}

func (g *ProtoBufSubTest) Descriptors() hbuf.Descriptor {
	return protoBufSubTestDescriptor
}

func (g *ProtoBufSubTest) GetV1() int8 {
	return g.V1
}

func (g *ProtoBufSubTest) SetV1(val int8) {
	g.V1 = val
}

func (g *ProtoBufSubTest) GetV2() int8 {
	if nil == g.V2 {
		return int8(0)
	}
	return *g.V2
}

func (g *ProtoBufSubTest) SetV2(val int8) {
	g.V2 = &val
}

var protoBufTest ProtoBufTest
var protoBufTestDescriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(&protoBufTest), map[uint16]hbuf.Descriptor{
	1:  hbuf.NewInt8Descriptor(unsafe.Offsetof(protoBufTest.V1), false),
	2:  hbuf.NewInt8Descriptor(unsafe.Offsetof(protoBufTest.V2), true),
	3:  hbuf.NewInt16Descriptor(unsafe.Offsetof(protoBufTest.V3), false),
	4:  hbuf.NewInt16Descriptor(unsafe.Offsetof(protoBufTest.V4), true),
	5:  hbuf.NewInt32Descriptor(unsafe.Offsetof(protoBufTest.V5), false),
	6:  hbuf.NewInt32Descriptor(unsafe.Offsetof(protoBufTest.V6), true),
	7:  hbuf.NewInt64Descriptor(unsafe.Offsetof(protoBufTest.V7), false),
	8:  hbuf.NewInt64Descriptor(unsafe.Offsetof(protoBufTest.V8), true),
	9:  hbuf.NewUint8Descriptor(unsafe.Offsetof(protoBufTest.V9), false),
	10: hbuf.NewUint8Descriptor(unsafe.Offsetof(protoBufTest.V10), true),
	11: hbuf.NewUint16Descriptor(unsafe.Offsetof(protoBufTest.V11), false),
	12: hbuf.NewUint16Descriptor(unsafe.Offsetof(protoBufTest.V12), true),
	13: hbuf.NewUint32Descriptor(unsafe.Offsetof(protoBufTest.V13), false),
	14: hbuf.NewUint32Descriptor(unsafe.Offsetof(protoBufTest.V14), true),
	15: hbuf.NewUint64Descriptor(unsafe.Offsetof(protoBufTest.V15), false),
	16: hbuf.NewUint64Descriptor(unsafe.Offsetof(protoBufTest.V16), true),
	17: hbuf.NewBoolDescriptor(unsafe.Offsetof(protoBufTest.V17), false),
	18: hbuf.NewBoolDescriptor(unsafe.Offsetof(protoBufTest.V18), true),
	19: hbuf.NewStringDescriptor(unsafe.Offsetof(protoBufTest.V19), false),
	20: hbuf.NewStringDescriptor(unsafe.Offsetof(protoBufTest.V20), true),
	23: hbuf.NewFloatDescriptor(unsafe.Offsetof(protoBufTest.V23), false),
	24: hbuf.NewFloatDescriptor(unsafe.Offsetof(protoBufTest.V24), true),
	25: hbuf.NewDoubleDescriptor(unsafe.Offsetof(protoBufTest.V25), false),
	26: hbuf.NewDoubleDescriptor(unsafe.Offsetof(protoBufTest.V26), true),
	27: hbuf.NewListDescriptor[int8](unsafe.Offsetof(protoBufTest.V27), hbuf.NewInt8Descriptor(0, false)),
	28: hbuf.NewListDescriptor[int8](unsafe.Offsetof(protoBufTest.V28), hbuf.NewInt8Descriptor(0, false)),
	29: hbuf.NewListDescriptor[int16](unsafe.Offsetof(protoBufTest.V29), hbuf.NewInt16Descriptor(0, false)),
	30: hbuf.NewListDescriptor[int16](unsafe.Offsetof(protoBufTest.V30), hbuf.NewInt16Descriptor(0, false)),
	31: hbuf.NewListDescriptor[int32](unsafe.Offsetof(protoBufTest.V31), hbuf.NewInt32Descriptor(0, false)),
	32: hbuf.NewListDescriptor[int32](unsafe.Offsetof(protoBufTest.V32), hbuf.NewInt32Descriptor(0, false)),
	33: hbuf.NewListDescriptor[hbuf.Int64](unsafe.Offsetof(protoBufTest.V33), hbuf.NewInt64Descriptor(0, false)),
	34: hbuf.NewListDescriptor[hbuf.Int64](unsafe.Offsetof(protoBufTest.V34), hbuf.NewInt64Descriptor(0, false)),
	35: hbuf.NewListDescriptor[uint8](unsafe.Offsetof(protoBufTest.V35), hbuf.NewUint8Descriptor(0, false)),
	36: hbuf.NewListDescriptor[uint8](unsafe.Offsetof(protoBufTest.V36), hbuf.NewUint8Descriptor(0, false)),
	37: hbuf.NewListDescriptor[uint16](unsafe.Offsetof(protoBufTest.V37), hbuf.NewUint16Descriptor(0, false)),
	38: hbuf.NewListDescriptor[uint16](unsafe.Offsetof(protoBufTest.V38), hbuf.NewUint16Descriptor(0, false)),
	39: hbuf.NewListDescriptor[uint32](unsafe.Offsetof(protoBufTest.V39), hbuf.NewUint32Descriptor(0, false)),
	40: hbuf.NewListDescriptor[uint32](unsafe.Offsetof(protoBufTest.V40), hbuf.NewUint32Descriptor(0, false)),
	41: hbuf.NewListDescriptor[hbuf.Uint64](unsafe.Offsetof(protoBufTest.V41), hbuf.NewUint64Descriptor(0, false)),
	42: hbuf.NewListDescriptor[hbuf.Uint64](unsafe.Offsetof(protoBufTest.V42), hbuf.NewUint64Descriptor(0, false)),
	43: hbuf.NewListDescriptor[bool](unsafe.Offsetof(protoBufTest.V43), hbuf.NewBoolDescriptor(0, false)),
	44: hbuf.NewListDescriptor[bool](unsafe.Offsetof(protoBufTest.V44), hbuf.NewBoolDescriptor(0, false)),
	45: hbuf.NewListDescriptor[string](unsafe.Offsetof(protoBufTest.V45), hbuf.NewStringDescriptor(0, false)),
	46: hbuf.NewListDescriptor[string](unsafe.Offsetof(protoBufTest.V46), hbuf.NewStringDescriptor(0, false)),
	49: hbuf.NewListDescriptor[float32](unsafe.Offsetof(protoBufTest.V49), hbuf.NewFloatDescriptor(0, false)),
	50: hbuf.NewListDescriptor[float32](unsafe.Offsetof(protoBufTest.V50), hbuf.NewFloatDescriptor(0, false)),
	51: hbuf.NewListDescriptor[float64](unsafe.Offsetof(protoBufTest.V51), hbuf.NewDoubleDescriptor(0, false)),
	52: hbuf.NewListDescriptor[float64](unsafe.Offsetof(protoBufTest.V52), hbuf.NewDoubleDescriptor(0, false)),
	53: hbuf.CloneDataDescriptor(&ProtoBufSubTest{}, unsafe.Offsetof(protoBufTest.V53), false),
	54: hbuf.CloneDataDescriptor(&ProtoBufSubTest{}, unsafe.Offsetof(protoBufTest.V54), true),
	55: hbuf.NewListDescriptor[*ProtoBufSubTest](unsafe.Offsetof(protoBufTest.V55), hbuf.CloneDataDescriptor(&ProtoBufSubTest{}, 0, false)),
	56: hbuf.NewListDescriptor[*ProtoBufSubTest](unsafe.Offsetof(protoBufTest.V56), hbuf.CloneDataDescriptor(&ProtoBufSubTest{}, 0, false)),
	57: hbuf.CloneDataDescriptor(&ProtoBufSubTest{}, unsafe.Offsetof(protoBufTest.V57), false),
})

type ProtoBufTest struct {
	V1  int8              `json:"v1,omitempty" hbuf:"1"`   //
	V2  *int8             `json:"v2,omitempty" hbuf:"2"`   //
	V3  int16             `json:"v3,omitempty" hbuf:"3"`   //
	V4  *int16            `json:"v4,omitempty" hbuf:"4"`   //
	V5  int32             `json:"v5,omitempty" hbuf:"5"`   //
	V6  *int32            `json:"v6,omitempty" hbuf:"6"`   //
	V7  hbuf.Int64        `json:"v7,omitempty" hbuf:"7"`   //
	V8  *hbuf.Int64       `json:"v8,omitempty" hbuf:"8"`   //
	V9  uint8             `json:"v9,omitempty" hbuf:"9"`   //
	V10 *uint8            `json:"v10,omitempty" hbuf:"10"` //
	V11 uint16            `json:"v11,omitempty" hbuf:"11"` //
	V12 *uint16           `json:"v12,omitempty" hbuf:"12"` //
	V13 uint32            `json:"v13,omitempty" hbuf:"13"` //
	V14 *uint32           `json:"v14,omitempty" hbuf:"14"` //
	V15 hbuf.Uint64       `json:"v15,omitempty" hbuf:"15"` //
	V16 *hbuf.Uint64      `json:"v16,omitempty" hbuf:"16"` //
	V17 bool              `json:"v17,omitempty" hbuf:"17"` //
	V18 *bool             `json:"v18,omitempty" hbuf:"18"` //
	V19 string            `json:"v19,omitempty" hbuf:"19"` //
	V20 *string           `json:"v20,omitempty" hbuf:"20"` //
	V23 float32           `json:"v23,omitempty" hbuf:"23"` //
	V24 *float32          `json:"v24,omitempty" hbuf:"24"` //
	V25 float64           `json:"v25,omitempty" hbuf:"25"` //
	V26 *float64          `json:"v26,omitempty" hbuf:"26"` //
	V27 []int8            `json:"v27,omitempty" hbuf:"27"` //
	V28 []int8            `json:"v28,omitempty" hbuf:"28"` //
	V29 []int16           `json:"v29,omitempty" hbuf:"29"` //
	V30 []int16           `json:"v30,omitempty" hbuf:"30"` //
	V31 []int32           `json:"v31,omitempty" hbuf:"31"` //
	V32 []int32           `json:"v32,omitempty" hbuf:"32"` //
	V33 []hbuf.Int64      `json:"v33,omitempty" hbuf:"33"` //
	V34 []hbuf.Int64      `json:"v34,omitempty" hbuf:"34"` //
	V35 []uint8           `json:"v35,omitempty" hbuf:"35"` //
	V36 []uint8           `json:"v36,omitempty" hbuf:"36"` //
	V37 []uint16          `json:"v37,omitempty" hbuf:"37"` //
	V38 []uint16          `json:"v38,omitempty" hbuf:"38"` //
	V39 []uint32          `json:"v39,omitempty" hbuf:"39"` //
	V40 []uint32          `json:"v40,omitempty" hbuf:"40"` //
	V41 []hbuf.Uint64     `json:"v41,omitempty" hbuf:"41"` //
	V42 []hbuf.Uint64     `json:"v42,omitempty" hbuf:"42"` //
	V43 []bool            `json:"v43,omitempty" hbuf:"43"` //
	V44 []bool            `json:"v44,omitempty" hbuf:"44"` //
	V45 []string          `json:"v45,omitempty" hbuf:"45"` //
	V46 []string          `json:"v46,omitempty" hbuf:"46"` //
	V49 []float32         `json:"v49,omitempty" hbuf:"49"` //
	V50 []float32         `json:"v50,omitempty" hbuf:"50"` //
	V51 []float64         `json:"v51,omitempty" hbuf:"51"` //
	V52 []float64         `json:"v52,omitempty" hbuf:"52"` //
	V53 ProtoBufSubTest   `json:"v53,omitempty" hbuf:"53"` //
	V54 *ProtoBufSubTest  `json:"v54,omitempty" hbuf:"54"` //
	V55 []ProtoBufSubTest `json:"v55,omitempty" hbuf:"55"` //
	V56 []ProtoBufSubTest `json:"v56,omitempty" hbuf:"56"` //
	V57 ProtoBufSubTest   `json:"v57,omitempty" hbuf:"57"` //
}

func (g *ProtoBufTest) Descriptors() hbuf.Descriptor {
	return protoBufTestDescriptor
}

func (g *ProtoBufTest) GetV1() int8 {
	return g.V1
}

func (g *ProtoBufTest) SetV1(val int8) {
	g.V1 = val
}

func (g *ProtoBufTest) GetV2() int8 {
	if nil == g.V2 {
		return int8(0)
	}
	return *g.V2
}

func (g *ProtoBufTest) SetV2(val int8) {
	g.V2 = &val
}

func (g *ProtoBufTest) GetV3() int16 {
	return g.V3
}

func (g *ProtoBufTest) SetV3(val int16) {
	g.V3 = val
}

func (g *ProtoBufTest) GetV4() int16 {
	if nil == g.V4 {
		return int16(0)
	}
	return *g.V4
}

func (g *ProtoBufTest) SetV4(val int16) {
	g.V4 = &val
}

func (g *ProtoBufTest) GetV5() int32 {
	return g.V5
}

func (g *ProtoBufTest) SetV5(val int32) {
	g.V5 = val
}

func (g *ProtoBufTest) GetV6() int32 {
	if nil == g.V6 {
		return int32(0)
	}
	return *g.V6
}

func (g *ProtoBufTest) SetV6(val int32) {
	g.V6 = &val
}

func (g *ProtoBufTest) GetV7() hbuf.Int64 {
	return g.V7
}

func (g *ProtoBufTest) SetV7(val hbuf.Int64) {
	g.V7 = val
}

func (g *ProtoBufTest) GetV8() hbuf.Int64 {
	if nil == g.V8 {
		return hbuf.Int64(0)
	}
	return *g.V8
}

func (g *ProtoBufTest) SetV8(val hbuf.Int64) {
	g.V8 = &val
}

func (g *ProtoBufTest) GetV9() uint8 {
	return g.V9
}

func (g *ProtoBufTest) SetV9(val uint8) {
	g.V9 = val
}

func (g *ProtoBufTest) GetV10() uint8 {
	if nil == g.V10 {
		return uint8(0)
	}
	return *g.V10
}

func (g *ProtoBufTest) SetV10(val uint8) {
	g.V10 = &val
}

func (g *ProtoBufTest) GetV11() uint16 {
	return g.V11
}

func (g *ProtoBufTest) SetV11(val uint16) {
	g.V11 = val
}

func (g *ProtoBufTest) GetV12() uint16 {
	if nil == g.V12 {
		return uint16(0)
	}
	return *g.V12
}

func (g *ProtoBufTest) SetV12(val uint16) {
	g.V12 = &val
}

func (g *ProtoBufTest) GetV13() uint32 {
	return g.V13
}

func (g *ProtoBufTest) SetV13(val uint32) {
	g.V13 = val
}

func (g *ProtoBufTest) GetV14() uint32 {
	if nil == g.V14 {
		return uint32(0)
	}
	return *g.V14
}

func (g *ProtoBufTest) SetV14(val uint32) {
	g.V14 = &val
}

func (g *ProtoBufTest) GetV15() hbuf.Uint64 {
	return g.V15
}

func (g *ProtoBufTest) SetV15(val hbuf.Uint64) {
	g.V15 = val
}

func (g *ProtoBufTest) GetV16() hbuf.Uint64 {
	if nil == g.V16 {
		return hbuf.Uint64(0)
	}
	return *g.V16
}

func (g *ProtoBufTest) SetV16(val hbuf.Uint64) {
	g.V16 = &val
}

func (g *ProtoBufTest) GetV17() bool {
	return g.V17
}

func (g *ProtoBufTest) SetV17(val bool) {
	g.V17 = val
}

func (g *ProtoBufTest) GetV18() bool {
	if nil == g.V18 {
		return false
	}
	return *g.V18
}

func (g *ProtoBufTest) SetV18(val bool) {
	g.V18 = &val
}

func (g *ProtoBufTest) GetV19() string {
	return g.V19
}

func (g *ProtoBufTest) SetV19(val string) {
	g.V19 = val
}

func (g *ProtoBufTest) GetV20() string {
	if nil == g.V20 {
		return ""
	}
	return *g.V20
}

func (g *ProtoBufTest) SetV20(val string) {
	g.V20 = &val
}

func (g *ProtoBufTest) GetV23() float32 {
	return g.V23
}

func (g *ProtoBufTest) SetV23(val float32) {
	g.V23 = val
}

func (g *ProtoBufTest) GetV24() float32 {
	if nil == g.V24 {
		return float32(0)
	}
	return *g.V24
}

func (g *ProtoBufTest) SetV24(val float32) {
	g.V24 = &val
}

func (g *ProtoBufTest) GetV25() float64 {
	return g.V25
}

func (g *ProtoBufTest) SetV25(val float64) {
	g.V25 = val
}

func (g *ProtoBufTest) GetV26() float64 {
	if nil == g.V26 {
		return float64(0)
	}
	return *g.V26
}

func (g *ProtoBufTest) SetV26(val float64) {
	g.V26 = &val
}

func (g *ProtoBufTest) GetV27() []int8 {
	return g.V27
}

func (g *ProtoBufTest) SetV27(val []int8) {
	g.V27 = val
}

func (g *ProtoBufTest) GetV28() []int8 {
	return g.V28
}

func (g *ProtoBufTest) SetV28(val []int8) {
	g.V28 = val
}

func (g *ProtoBufTest) GetV29() []int16 {
	return g.V29
}

func (g *ProtoBufTest) SetV29(val []int16) {
	g.V29 = val
}

func (g *ProtoBufTest) GetV30() []int16 {
	return g.V30
}

func (g *ProtoBufTest) SetV30(val []int16) {
	g.V30 = val
}

func (g *ProtoBufTest) GetV31() []int32 {
	return g.V31
}

func (g *ProtoBufTest) SetV31(val []int32) {
	g.V31 = val
}

func (g *ProtoBufTest) GetV32() []int32 {
	return g.V32
}

func (g *ProtoBufTest) SetV32(val []int32) {
	g.V32 = val
}

func (g *ProtoBufTest) GetV33() []hbuf.Int64 {
	return g.V33
}

func (g *ProtoBufTest) SetV33(val []hbuf.Int64) {
	g.V33 = val
}

func (g *ProtoBufTest) GetV34() []hbuf.Int64 {
	return g.V34
}

func (g *ProtoBufTest) SetV34(val []hbuf.Int64) {
	g.V34 = val
}

func (g *ProtoBufTest) GetV35() []uint8 {
	return g.V35
}

func (g *ProtoBufTest) SetV35(val []uint8) {
	g.V35 = val
}

func (g *ProtoBufTest) GetV36() []uint8 {
	return g.V36
}

func (g *ProtoBufTest) SetV36(val []uint8) {
	g.V36 = val
}

func (g *ProtoBufTest) GetV37() []uint16 {
	return g.V37
}

func (g *ProtoBufTest) SetV37(val []uint16) {
	g.V37 = val
}

func (g *ProtoBufTest) GetV38() []uint16 {
	return g.V38
}

func (g *ProtoBufTest) SetV38(val []uint16) {
	g.V38 = val
}

func (g *ProtoBufTest) GetV39() []uint32 {
	return g.V39
}

func (g *ProtoBufTest) SetV39(val []uint32) {
	g.V39 = val
}

func (g *ProtoBufTest) GetV40() []uint32 {
	return g.V40
}

func (g *ProtoBufTest) SetV40(val []uint32) {
	g.V40 = val
}

func (g *ProtoBufTest) GetV41() []hbuf.Uint64 {
	return g.V41
}

func (g *ProtoBufTest) SetV41(val []hbuf.Uint64) {
	g.V41 = val
}

func (g *ProtoBufTest) GetV42() []hbuf.Uint64 {
	return g.V42
}

func (g *ProtoBufTest) SetV42(val []hbuf.Uint64) {
	g.V42 = val
}

func (g *ProtoBufTest) GetV43() []bool {
	return g.V43
}

func (g *ProtoBufTest) SetV43(val []bool) {
	g.V43 = val
}

func (g *ProtoBufTest) GetV44() []bool {
	return g.V44
}

func (g *ProtoBufTest) SetV44(val []bool) {
	g.V44 = val
}

func (g *ProtoBufTest) GetV45() []string {
	return g.V45
}

func (g *ProtoBufTest) SetV45(val []string) {
	g.V45 = val
}

func (g *ProtoBufTest) GetV46() []string {
	return g.V46
}

func (g *ProtoBufTest) SetV46(val []string) {
	g.V46 = val
}

func (g *ProtoBufTest) GetV49() []float32 {
	return g.V49
}

func (g *ProtoBufTest) SetV49(val []float32) {
	g.V49 = val
}

func (g *ProtoBufTest) GetV50() []float32 {
	return g.V50
}

func (g *ProtoBufTest) SetV50(val []float32) {
	g.V50 = val
}

func (g *ProtoBufTest) GetV51() []float64 {
	return g.V51
}

func (g *ProtoBufTest) SetV51(val []float64) {
	g.V51 = val
}

func (g *ProtoBufTest) GetV52() []float64 {
	return g.V52
}

func (g *ProtoBufTest) SetV52(val []float64) {
	g.V52 = val
}

func (g *ProtoBufTest) GetV53() ProtoBufSubTest {
	return g.V53
}

func (g *ProtoBufTest) SetV53(val ProtoBufSubTest) {
	g.V53 = val
}

func (g *ProtoBufTest) GetV54() ProtoBufSubTest {
	if nil == g.V54 {
		return ProtoBufSubTest{}
	}
	return *g.V54
}

func (g *ProtoBufTest) SetV54(val ProtoBufSubTest) {
	g.V54 = &val
}

func (g *ProtoBufTest) GetV55() []ProtoBufSubTest {
	return g.V55
}

func (g *ProtoBufTest) SetV55(val []ProtoBufSubTest) {
	g.V55 = val
}

func (g *ProtoBufTest) GetV56() []ProtoBufSubTest {
	return g.V56
}

func (g *ProtoBufTest) SetV56(val []ProtoBufSubTest) {
	g.V56 = val
}

func (g *ProtoBufTest) GetV57() ProtoBufSubTest {
	return g.V57
}

func (g *ProtoBufTest) SetV57(val ProtoBufSubTest) {
	g.V57 = val
}
