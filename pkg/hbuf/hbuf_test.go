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
	1:   hbuf.NewInt8Descriptor(unsafe.Offsetof(hBufTest.V1), false),
	2:   hbuf.NewInt8Descriptor(unsafe.Offsetof(hBufTest.V2), true),
	3:   hbuf.NewInt16Descriptor(unsafe.Offsetof(hBufTest.V3), false),
	4:   hbuf.NewInt16Descriptor(unsafe.Offsetof(hBufTest.V4), true),
	5:   hbuf.NewInt32Descriptor(unsafe.Offsetof(hBufTest.V5), false),
	6:   hbuf.NewInt32Descriptor(unsafe.Offsetof(hBufTest.V6), true),
	7:   hbuf.NewInt64Descriptor(unsafe.Offsetof(hBufTest.V7), false),
	8:   hbuf.NewInt64Descriptor(unsafe.Offsetof(hBufTest.V8), true),
	9:   hbuf.NewUint8Descriptor(unsafe.Offsetof(hBufTest.V9), false),
	10:  hbuf.NewUint8Descriptor(unsafe.Offsetof(hBufTest.V10), true),
	11:  hbuf.NewUint16Descriptor(unsafe.Offsetof(hBufTest.V11), false),
	12:  hbuf.NewUint16Descriptor(unsafe.Offsetof(hBufTest.V12), true),
	13:  hbuf.NewUint32Descriptor(unsafe.Offsetof(hBufTest.V13), false),
	14:  hbuf.NewUint32Descriptor(unsafe.Offsetof(hBufTest.V14), true),
	15:  hbuf.NewUint64Descriptor(unsafe.Offsetof(hBufTest.V15), false),
	16:  hbuf.NewUint64Descriptor(unsafe.Offsetof(hBufTest.V16), true),
	17:  hbuf.NewBoolDescriptor(unsafe.Offsetof(hBufTest.V17), false),
	18:  hbuf.NewBoolDescriptor(unsafe.Offsetof(hBufTest.V18), true),
	19:  hbuf.NewStringDescriptor(unsafe.Offsetof(hBufTest.V19), false),
	20:  hbuf.NewStringDescriptor(unsafe.Offsetof(hBufTest.V20), true),
	21:  hbuf.NewBytesDescriptor(unsafe.Offsetof(hBufTest.V21), false),
	22:  hbuf.NewBytesDescriptor(unsafe.Offsetof(hBufTest.V22), true),
	23:  hbuf.NewFloatDescriptor(unsafe.Offsetof(hBufTest.V23), false),
	24:  hbuf.NewFloatDescriptor(unsafe.Offsetof(hBufTest.V24), true),
	25:  hbuf.NewDoubleDescriptor(unsafe.Offsetof(hBufTest.V25), false),
	26:  hbuf.NewDoubleDescriptor(unsafe.Offsetof(hBufTest.V26), true),
	27:  hbuf.NewTimeDescriptor(unsafe.Offsetof(hBufTest.V27), false),
	28:  hbuf.NewTimeDescriptor(unsafe.Offsetof(hBufTest.V28), true),
	29:  hbuf.NewDecimalDescriptor(unsafe.Offsetof(hBufTest.V29), false),
	30:  hbuf.NewDecimalDescriptor(unsafe.Offsetof(hBufTest.V30), true),
	31:  hbuf.CloneDataDescriptor(&HBufSubTest{}, unsafe.Offsetof(hBufTest.V31), false),
	32:  hbuf.CloneDataDescriptor(&HBufSubTest{}, unsafe.Offsetof(hBufTest.V32), true),
	33:  hbuf.NewListDescriptor[int8](unsafe.Offsetof(hBufTest.V33), hbuf.NewInt8Descriptor(0, false), false),
	34:  hbuf.NewListDescriptor[*int8](unsafe.Offsetof(hBufTest.V34), hbuf.NewInt8Descriptor(0, true), false),
	35:  hbuf.NewListDescriptor[int16](unsafe.Offsetof(hBufTest.V35), hbuf.NewInt16Descriptor(0, false), false),
	36:  hbuf.NewListDescriptor[*int16](unsafe.Offsetof(hBufTest.V36), hbuf.NewInt16Descriptor(0, true), false),
	37:  hbuf.NewListDescriptor[int32](unsafe.Offsetof(hBufTest.V37), hbuf.NewInt32Descriptor(0, false), false),
	38:  hbuf.NewListDescriptor[*int32](unsafe.Offsetof(hBufTest.V38), hbuf.NewInt32Descriptor(0, true), false),
	39:  hbuf.NewListDescriptor[hbuf.Int64](unsafe.Offsetof(hBufTest.V39), hbuf.NewInt64Descriptor(0, false), false),
	40:  hbuf.NewListDescriptor[*hbuf.Int64](unsafe.Offsetof(hBufTest.V40), hbuf.NewInt64Descriptor(0, true), false),
	41:  hbuf.NewListDescriptor[uint8](unsafe.Offsetof(hBufTest.V41), hbuf.NewUint8Descriptor(0, false), false),
	42:  hbuf.NewListDescriptor[*uint8](unsafe.Offsetof(hBufTest.V42), hbuf.NewUint8Descriptor(0, true), false),
	43:  hbuf.NewListDescriptor[uint16](unsafe.Offsetof(hBufTest.V43), hbuf.NewUint16Descriptor(0, false), false),
	44:  hbuf.NewListDescriptor[*uint16](unsafe.Offsetof(hBufTest.V44), hbuf.NewUint16Descriptor(0, true), false),
	45:  hbuf.NewListDescriptor[uint32](unsafe.Offsetof(hBufTest.V45), hbuf.NewUint32Descriptor(0, false), false),
	46:  hbuf.NewListDescriptor[*uint32](unsafe.Offsetof(hBufTest.V46), hbuf.NewUint32Descriptor(0, true), false),
	47:  hbuf.NewListDescriptor[hbuf.Uint64](unsafe.Offsetof(hBufTest.V47), hbuf.NewUint64Descriptor(0, false), false),
	48:  hbuf.NewListDescriptor[*hbuf.Uint64](unsafe.Offsetof(hBufTest.V48), hbuf.NewUint64Descriptor(0, true), false),
	49:  hbuf.NewListDescriptor[bool](unsafe.Offsetof(hBufTest.V49), hbuf.NewBoolDescriptor(0, false), false),
	50:  hbuf.NewListDescriptor[*bool](unsafe.Offsetof(hBufTest.V50), hbuf.NewBoolDescriptor(0, true), false),
	51:  hbuf.NewListDescriptor[string](unsafe.Offsetof(hBufTest.V51), hbuf.NewStringDescriptor(0, false), false),
	52:  hbuf.NewListDescriptor[*string](unsafe.Offsetof(hBufTest.V52), hbuf.NewStringDescriptor(0, true), false),
	53:  hbuf.NewListDescriptor[[]byte](unsafe.Offsetof(hBufTest.V53), hbuf.NewBytesDescriptor(0, false), false),
	54:  hbuf.NewListDescriptor[*[]byte](unsafe.Offsetof(hBufTest.V54), hbuf.NewBytesDescriptor(0, true), false),
	55:  hbuf.NewListDescriptor[float32](unsafe.Offsetof(hBufTest.V55), hbuf.NewFloatDescriptor(0, false), false),
	56:  hbuf.NewListDescriptor[*float32](unsafe.Offsetof(hBufTest.V56), hbuf.NewFloatDescriptor(0, true), false),
	57:  hbuf.NewListDescriptor[float64](unsafe.Offsetof(hBufTest.V57), hbuf.NewDoubleDescriptor(0, false), false),
	58:  hbuf.NewListDescriptor[*float64](unsafe.Offsetof(hBufTest.V58), hbuf.NewDoubleDescriptor(0, true), false),
	59:  hbuf.NewListDescriptor[hbuf.Time](unsafe.Offsetof(hBufTest.V59), hbuf.NewTimeDescriptor(0, false), false),
	60:  hbuf.NewListDescriptor[*hbuf.Time](unsafe.Offsetof(hBufTest.V60), hbuf.NewTimeDescriptor(0, true), false),
	61:  hbuf.NewListDescriptor[decimal.Decimal](unsafe.Offsetof(hBufTest.V61), hbuf.NewDecimalDescriptor(0, false), false),
	62:  hbuf.NewListDescriptor[*decimal.Decimal](unsafe.Offsetof(hBufTest.V62), hbuf.NewDecimalDescriptor(0, true), false),
	63:  hbuf.NewListDescriptor[HBufSubTest](unsafe.Offsetof(hBufTest.V63), hbuf.CloneDataDescriptor(&HBufSubTest{}, 0, false), false),
	64:  hbuf.NewListDescriptor[*HBufSubTest](unsafe.Offsetof(hBufTest.V64), hbuf.CloneDataDescriptor(&HBufSubTest{}, 0, true), false),
	65:  hbuf.NewMapDescriptor[hbuf.Int64, int8](unsafe.Offsetof(hBufTest.V65), hbuf.NewInt64Descriptor(0, false), hbuf.NewInt8Descriptor(0, false), false),
	66:  hbuf.NewMapDescriptor[hbuf.Int64, *int8](unsafe.Offsetof(hBufTest.V66), hbuf.NewInt64Descriptor(0, false), hbuf.NewInt8Descriptor(0, true), false),
	67:  hbuf.NewMapDescriptor[hbuf.Int64, int16](unsafe.Offsetof(hBufTest.V67), hbuf.NewInt64Descriptor(0, false), hbuf.NewInt16Descriptor(0, false), false),
	68:  hbuf.NewMapDescriptor[hbuf.Int64, *int16](unsafe.Offsetof(hBufTest.V68), hbuf.NewInt64Descriptor(0, false), hbuf.NewInt16Descriptor(0, true), false),
	69:  hbuf.NewMapDescriptor[hbuf.Int64, int32](unsafe.Offsetof(hBufTest.V69), hbuf.NewInt64Descriptor(0, false), hbuf.NewInt32Descriptor(0, false), false),
	70:  hbuf.NewMapDescriptor[hbuf.Int64, *int32](unsafe.Offsetof(hBufTest.V70), hbuf.NewInt64Descriptor(0, false), hbuf.NewInt32Descriptor(0, true), false),
	71:  hbuf.NewMapDescriptor[hbuf.Int64, hbuf.Int64](unsafe.Offsetof(hBufTest.V71), hbuf.NewInt64Descriptor(0, false), hbuf.NewInt64Descriptor(0, false), false),
	72:  hbuf.NewMapDescriptor[hbuf.Int64, *hbuf.Int64](unsafe.Offsetof(hBufTest.V72), hbuf.NewInt64Descriptor(0, false), hbuf.NewInt64Descriptor(0, true), false),
	73:  hbuf.NewMapDescriptor[hbuf.Int64, uint8](unsafe.Offsetof(hBufTest.V73), hbuf.NewInt64Descriptor(0, false), hbuf.NewUint8Descriptor(0, false), false),
	74:  hbuf.NewMapDescriptor[hbuf.Int64, *uint8](unsafe.Offsetof(hBufTest.V74), hbuf.NewInt64Descriptor(0, false), hbuf.NewUint8Descriptor(0, true), false),
	75:  hbuf.NewMapDescriptor[hbuf.Int64, uint16](unsafe.Offsetof(hBufTest.V75), hbuf.NewInt64Descriptor(0, false), hbuf.NewUint16Descriptor(0, false), false),
	76:  hbuf.NewMapDescriptor[hbuf.Int64, *uint16](unsafe.Offsetof(hBufTest.V76), hbuf.NewInt64Descriptor(0, false), hbuf.NewUint16Descriptor(0, true), false),
	77:  hbuf.NewMapDescriptor[hbuf.Int64, uint32](unsafe.Offsetof(hBufTest.V77), hbuf.NewInt64Descriptor(0, false), hbuf.NewUint32Descriptor(0, false), false),
	78:  hbuf.NewMapDescriptor[hbuf.Int64, *uint32](unsafe.Offsetof(hBufTest.V78), hbuf.NewInt64Descriptor(0, false), hbuf.NewUint32Descriptor(0, true), false),
	79:  hbuf.NewMapDescriptor[hbuf.Int64, hbuf.Uint64](unsafe.Offsetof(hBufTest.V79), hbuf.NewInt64Descriptor(0, false), hbuf.NewUint64Descriptor(0, false), false),
	80:  hbuf.NewMapDescriptor[hbuf.Int64, *hbuf.Uint64](unsafe.Offsetof(hBufTest.V80), hbuf.NewInt64Descriptor(0, false), hbuf.NewUint64Descriptor(0, true), false),
	81:  hbuf.NewMapDescriptor[hbuf.Int64, bool](unsafe.Offsetof(hBufTest.V81), hbuf.NewInt64Descriptor(0, false), hbuf.NewBoolDescriptor(0, false), false),
	82:  hbuf.NewMapDescriptor[hbuf.Int64, *bool](unsafe.Offsetof(hBufTest.V82), hbuf.NewInt64Descriptor(0, false), hbuf.NewBoolDescriptor(0, true), false),
	83:  hbuf.NewMapDescriptor[hbuf.Int64, string](unsafe.Offsetof(hBufTest.V83), hbuf.NewInt64Descriptor(0, false), hbuf.NewStringDescriptor(0, false), false),
	84:  hbuf.NewMapDescriptor[hbuf.Int64, *string](unsafe.Offsetof(hBufTest.V84), hbuf.NewInt64Descriptor(0, false), hbuf.NewStringDescriptor(0, true), false),
	85:  hbuf.NewMapDescriptor[hbuf.Int64, []byte](unsafe.Offsetof(hBufTest.V85), hbuf.NewInt64Descriptor(0, false), hbuf.NewBytesDescriptor(0, false), false),
	86:  hbuf.NewMapDescriptor[hbuf.Int64, *[]byte](unsafe.Offsetof(hBufTest.V86), hbuf.NewInt64Descriptor(0, false), hbuf.NewBytesDescriptor(0, true), false),
	87:  hbuf.NewMapDescriptor[hbuf.Int64, float32](unsafe.Offsetof(hBufTest.V87), hbuf.NewInt64Descriptor(0, false), hbuf.NewFloatDescriptor(0, false), false),
	88:  hbuf.NewMapDescriptor[hbuf.Int64, *float32](unsafe.Offsetof(hBufTest.V88), hbuf.NewInt64Descriptor(0, false), hbuf.NewFloatDescriptor(0, true), false),
	89:  hbuf.NewMapDescriptor[hbuf.Int64, float64](unsafe.Offsetof(hBufTest.V89), hbuf.NewInt64Descriptor(0, false), hbuf.NewDoubleDescriptor(0, false), false),
	90:  hbuf.NewMapDescriptor[hbuf.Int64, *float64](unsafe.Offsetof(hBufTest.V90), hbuf.NewInt64Descriptor(0, false), hbuf.NewDoubleDescriptor(0, true), false),
	91:  hbuf.NewMapDescriptor[hbuf.Int64, hbuf.Time](unsafe.Offsetof(hBufTest.V91), hbuf.NewInt64Descriptor(0, false), hbuf.NewTimeDescriptor(0, false), false),
	92:  hbuf.NewMapDescriptor[hbuf.Int64, *hbuf.Time](unsafe.Offsetof(hBufTest.V92), hbuf.NewInt64Descriptor(0, false), hbuf.NewTimeDescriptor(0, true), false),
	93:  hbuf.NewMapDescriptor[hbuf.Int64, decimal.Decimal](unsafe.Offsetof(hBufTest.V93), hbuf.NewInt64Descriptor(0, false), hbuf.NewDecimalDescriptor(0, false), false),
	94:  hbuf.NewMapDescriptor[hbuf.Int64, *decimal.Decimal](unsafe.Offsetof(hBufTest.V94), hbuf.NewInt64Descriptor(0, false), hbuf.NewDecimalDescriptor(0, true), false),
	95:  hbuf.NewMapDescriptor[hbuf.Int64, HBufSubTest](unsafe.Offsetof(hBufTest.V95), hbuf.NewInt64Descriptor(0, false), hbuf.CloneDataDescriptor(&HBufSubTest{}, 0, false), false),
	96:  hbuf.NewMapDescriptor[hbuf.Int64, *HBufSubTest](unsafe.Offsetof(hBufTest.V96), hbuf.NewInt64Descriptor(0, false), hbuf.CloneDataDescriptor(&HBufSubTest{}, 0, true), false),
	97:  hbuf.NewMapDescriptor[string, int8](unsafe.Offsetof(hBufTest.V97), hbuf.NewStringDescriptor(0, false), hbuf.NewInt8Descriptor(0, false), false),
	98:  hbuf.NewMapDescriptor[string, *int8](unsafe.Offsetof(hBufTest.V98), hbuf.NewStringDescriptor(0, false), hbuf.NewInt8Descriptor(0, true), false),
	99:  hbuf.NewMapDescriptor[string, int16](unsafe.Offsetof(hBufTest.V99), hbuf.NewStringDescriptor(0, false), hbuf.NewInt16Descriptor(0, false), false),
	100: hbuf.NewMapDescriptor[string, *int16](unsafe.Offsetof(hBufTest.V100), hbuf.NewStringDescriptor(0, false), hbuf.NewInt16Descriptor(0, true), false),
	101: hbuf.NewMapDescriptor[string, int32](unsafe.Offsetof(hBufTest.V101), hbuf.NewStringDescriptor(0, false), hbuf.NewInt32Descriptor(0, false), false),
	102: hbuf.NewMapDescriptor[string, *int32](unsafe.Offsetof(hBufTest.V102), hbuf.NewStringDescriptor(0, false), hbuf.NewInt32Descriptor(0, true), false),
	103: hbuf.NewMapDescriptor[string, hbuf.Int64](unsafe.Offsetof(hBufTest.V103), hbuf.NewStringDescriptor(0, false), hbuf.NewInt64Descriptor(0, false), false),
	104: hbuf.NewMapDescriptor[string, *hbuf.Int64](unsafe.Offsetof(hBufTest.V104), hbuf.NewStringDescriptor(0, false), hbuf.NewInt64Descriptor(0, true), false),
	105: hbuf.NewMapDescriptor[string, uint8](unsafe.Offsetof(hBufTest.V105), hbuf.NewStringDescriptor(0, false), hbuf.NewUint8Descriptor(0, false), false),
	106: hbuf.NewMapDescriptor[string, *uint8](unsafe.Offsetof(hBufTest.V106), hbuf.NewStringDescriptor(0, false), hbuf.NewUint8Descriptor(0, true), false),
	107: hbuf.NewMapDescriptor[string, uint16](unsafe.Offsetof(hBufTest.V107), hbuf.NewStringDescriptor(0, false), hbuf.NewUint16Descriptor(0, false), false),
	108: hbuf.NewMapDescriptor[string, *uint16](unsafe.Offsetof(hBufTest.V108), hbuf.NewStringDescriptor(0, false), hbuf.NewUint16Descriptor(0, true), false),
	109: hbuf.NewMapDescriptor[string, uint32](unsafe.Offsetof(hBufTest.V109), hbuf.NewStringDescriptor(0, false), hbuf.NewUint32Descriptor(0, false), false),
	110: hbuf.NewMapDescriptor[string, *uint32](unsafe.Offsetof(hBufTest.V110), hbuf.NewStringDescriptor(0, false), hbuf.NewUint32Descriptor(0, true), false),
	111: hbuf.NewMapDescriptor[string, hbuf.Uint64](unsafe.Offsetof(hBufTest.V111), hbuf.NewStringDescriptor(0, false), hbuf.NewUint64Descriptor(0, false), false),
	112: hbuf.NewMapDescriptor[string, *hbuf.Uint64](unsafe.Offsetof(hBufTest.V112), hbuf.NewStringDescriptor(0, false), hbuf.NewUint64Descriptor(0, true), false),
	113: hbuf.NewMapDescriptor[string, bool](unsafe.Offsetof(hBufTest.V113), hbuf.NewStringDescriptor(0, false), hbuf.NewBoolDescriptor(0, false), false),
	114: hbuf.NewMapDescriptor[string, *bool](unsafe.Offsetof(hBufTest.V114), hbuf.NewStringDescriptor(0, false), hbuf.NewBoolDescriptor(0, true), false),
	115: hbuf.NewMapDescriptor[string, string](unsafe.Offsetof(hBufTest.V115), hbuf.NewStringDescriptor(0, false), hbuf.NewStringDescriptor(0, false), false),
	116: hbuf.NewMapDescriptor[string, *string](unsafe.Offsetof(hBufTest.V116), hbuf.NewStringDescriptor(0, false), hbuf.NewStringDescriptor(0, true), false),
	117: hbuf.NewMapDescriptor[string, []byte](unsafe.Offsetof(hBufTest.V117), hbuf.NewStringDescriptor(0, false), hbuf.NewBytesDescriptor(0, false), false),
	118: hbuf.NewMapDescriptor[string, *[]byte](unsafe.Offsetof(hBufTest.V118), hbuf.NewStringDescriptor(0, false), hbuf.NewBytesDescriptor(0, true), false),
	119: hbuf.NewMapDescriptor[string, float32](unsafe.Offsetof(hBufTest.V119), hbuf.NewStringDescriptor(0, false), hbuf.NewFloatDescriptor(0, false), false),
	120: hbuf.NewMapDescriptor[string, *float32](unsafe.Offsetof(hBufTest.V120), hbuf.NewStringDescriptor(0, false), hbuf.NewFloatDescriptor(0, true), false),
	121: hbuf.NewMapDescriptor[string, float64](unsafe.Offsetof(hBufTest.V121), hbuf.NewStringDescriptor(0, false), hbuf.NewDoubleDescriptor(0, false), false),
	122: hbuf.NewMapDescriptor[string, *float64](unsafe.Offsetof(hBufTest.V122), hbuf.NewStringDescriptor(0, false), hbuf.NewDoubleDescriptor(0, true), false),
	123: hbuf.NewMapDescriptor[string, hbuf.Time](unsafe.Offsetof(hBufTest.V123), hbuf.NewStringDescriptor(0, false), hbuf.NewTimeDescriptor(0, false), false),
	124: hbuf.NewMapDescriptor[string, *hbuf.Time](unsafe.Offsetof(hBufTest.V124), hbuf.NewStringDescriptor(0, false), hbuf.NewTimeDescriptor(0, true), false),
	125: hbuf.NewMapDescriptor[string, decimal.Decimal](unsafe.Offsetof(hBufTest.V125), hbuf.NewStringDescriptor(0, false), hbuf.NewDecimalDescriptor(0, false), false),
	126: hbuf.NewMapDescriptor[string, *decimal.Decimal](unsafe.Offsetof(hBufTest.V126), hbuf.NewStringDescriptor(0, false), hbuf.NewDecimalDescriptor(0, true), false),
	127: hbuf.NewMapDescriptor[string, HBufSubTest](unsafe.Offsetof(hBufTest.V127), hbuf.NewStringDescriptor(0, false), hbuf.CloneDataDescriptor(&HBufSubTest{}, 0, false), false),
	128: hbuf.NewMapDescriptor[string, *HBufSubTest](unsafe.Offsetof(hBufTest.V128), hbuf.NewStringDescriptor(0, false), hbuf.CloneDataDescriptor(&HBufSubTest{}, 0, true), false),
})

type HBufTest struct {
	V1   int8                            `json:"v1,omitempty" hbuf:"1"`     //
	V2   *int8                           `json:"v2,omitempty" hbuf:"2"`     //
	V3   int16                           `json:"v3,omitempty" hbuf:"3"`     //
	V4   *int16                          `json:"v4,omitempty" hbuf:"4"`     //
	V5   int32                           `json:"v5,omitempty" hbuf:"5"`     //
	V6   *int32                          `json:"v6,omitempty" hbuf:"6"`     //
	V7   hbuf.Int64                      `json:"v7,omitempty" hbuf:"7"`     //
	V8   *hbuf.Int64                     `json:"v8,omitempty" hbuf:"8"`     //
	V9   uint8                           `json:"v9,omitempty" hbuf:"9"`     //
	V10  *uint8                          `json:"v10,omitempty" hbuf:"10"`   //
	V11  uint16                          `json:"v11,omitempty" hbuf:"11"`   //
	V12  *uint16                         `json:"v12,omitempty" hbuf:"12"`   //
	V13  uint32                          `json:"v13,omitempty" hbuf:"13"`   //
	V14  *uint32                         `json:"v14,omitempty" hbuf:"14"`   //
	V15  hbuf.Uint64                     `json:"v15,omitempty" hbuf:"15"`   //
	V16  *hbuf.Uint64                    `json:"v16,omitempty" hbuf:"16"`   //
	V17  bool                            `json:"v17,omitempty" hbuf:"17"`   //
	V18  *bool                           `json:"v18,omitempty" hbuf:"18"`   //
	V19  string                          `json:"v19,omitempty" hbuf:"19"`   //
	V20  *string                         `json:"v20,omitempty" hbuf:"20"`   //
	V21  []byte                          `json:"v21,omitempty" hbuf:"21"`   //
	V22  *[]byte                         `json:"v22,omitempty" hbuf:"22"`   //
	V23  float32                         `json:"v23,omitempty" hbuf:"23"`   //
	V24  *float32                        `json:"v24,omitempty" hbuf:"24"`   //
	V25  float64                         `json:"v25,omitempty" hbuf:"25"`   //
	V26  *float64                        `json:"v26,omitempty" hbuf:"26"`   //
	V27  hbuf.Time                       `json:"v27,omitempty" hbuf:"27"`   //
	V28  *hbuf.Time                      `json:"v28,omitempty" hbuf:"28"`   //
	V29  decimal.Decimal                 `json:"v29,omitempty" hbuf:"29"`   //
	V30  *decimal.Decimal                `json:"v30,omitempty" hbuf:"30"`   //
	V31  HBufSubTest                     `json:"v31,omitempty" hbuf:"31"`   //
	V32  *HBufSubTest                    `json:"v32,omitempty" hbuf:"32"`   //
	V33  []int8                          `json:"v33,omitempty" hbuf:"33"`   //
	V34  []*int8                         `json:"v34,omitempty" hbuf:"34"`   //
	V35  []int16                         `json:"v35,omitempty" hbuf:"35"`   //
	V36  []*int16                        `json:"v36,omitempty" hbuf:"36"`   //
	V37  []int32                         `json:"v37,omitempty" hbuf:"37"`   //
	V38  []*int32                        `json:"v38,omitempty" hbuf:"38"`   //
	V39  []hbuf.Int64                    `json:"v39,omitempty" hbuf:"39"`   //
	V40  []*hbuf.Int64                   `json:"v40,omitempty" hbuf:"40"`   //
	V41  []uint8                         `json:"v41,omitempty" hbuf:"41"`   //
	V42  []*uint8                        `json:"v42,omitempty" hbuf:"42"`   //
	V43  []uint16                        `json:"v43,omitempty" hbuf:"43"`   //
	V44  []*uint16                       `json:"v44,omitempty" hbuf:"44"`   //
	V45  []uint32                        `json:"v45,omitempty" hbuf:"45"`   //
	V46  []*uint32                       `json:"v46,omitempty" hbuf:"46"`   //
	V47  []hbuf.Uint64                   `json:"v47,omitempty" hbuf:"47"`   //
	V48  []*hbuf.Uint64                  `json:"v48,omitempty" hbuf:"48"`   //
	V49  []bool                          `json:"v49,omitempty" hbuf:"49"`   //
	V50  []*bool                         `json:"v50,omitempty" hbuf:"50"`   //
	V51  []string                        `json:"v51,omitempty" hbuf:"51"`   //
	V52  []*string                       `json:"v52,omitempty" hbuf:"52"`   //
	V53  [][]byte                        `json:"v53,omitempty" hbuf:"53"`   //
	V54  []*[]byte                       `json:"v54,omitempty" hbuf:"54"`   //
	V55  []float32                       `json:"v55,omitempty" hbuf:"55"`   //
	V56  []*float32                      `json:"v56,omitempty" hbuf:"56"`   //
	V57  []float64                       `json:"v57,omitempty" hbuf:"57"`   //
	V58  []*float64                      `json:"v58,omitempty" hbuf:"58"`   //
	V59  []hbuf.Time                     `json:"v59,omitempty" hbuf:"59"`   //
	V60  []*hbuf.Time                    `json:"v60,omitempty" hbuf:"60"`   //
	V61  []decimal.Decimal               `json:"v61,omitempty" hbuf:"61"`   //
	V62  []*decimal.Decimal              `json:"v62,omitempty" hbuf:"62"`   //
	V63  []HBufSubTest                   `json:"v63,omitempty" hbuf:"63"`   //
	V64  []*HBufSubTest                  `json:"v64,omitempty" hbuf:"64"`   //
	V65  map[hbuf.Int64]int8             `json:"v65,omitempty" hbuf:"65"`   //
	V66  map[hbuf.Int64]*int8            `json:"v66,omitempty" hbuf:"66"`   //
	V67  map[hbuf.Int64]int16            `json:"v67,omitempty" hbuf:"67"`   //
	V68  map[hbuf.Int64]*int16           `json:"v68,omitempty" hbuf:"68"`   //
	V69  map[hbuf.Int64]int32            `json:"v69,omitempty" hbuf:"69"`   //
	V70  map[hbuf.Int64]*int32           `json:"v70,omitempty" hbuf:"70"`   //
	V71  map[hbuf.Int64]hbuf.Int64       `json:"v71,omitempty" hbuf:"71"`   //
	V72  map[hbuf.Int64]*hbuf.Int64      `json:"v72,omitempty" hbuf:"72"`   //
	V73  map[hbuf.Int64]uint8            `json:"v73,omitempty" hbuf:"73"`   //
	V74  map[hbuf.Int64]*uint8           `json:"v74,omitempty" hbuf:"74"`   //
	V75  map[hbuf.Int64]uint16           `json:"v75,omitempty" hbuf:"75"`   //
	V76  map[hbuf.Int64]*uint16          `json:"v76,omitempty" hbuf:"76"`   //
	V77  map[hbuf.Int64]uint32           `json:"v77,omitempty" hbuf:"77"`   //
	V78  map[hbuf.Int64]*uint32          `json:"v78,omitempty" hbuf:"78"`   //
	V79  map[hbuf.Int64]hbuf.Uint64      `json:"v79,omitempty" hbuf:"79"`   //
	V80  map[hbuf.Int64]*hbuf.Uint64     `json:"v80,omitempty" hbuf:"80"`   //
	V81  map[hbuf.Int64]bool             `json:"v81,omitempty" hbuf:"81"`   //
	V82  map[hbuf.Int64]*bool            `json:"v82,omitempty" hbuf:"82"`   //
	V83  map[hbuf.Int64]string           `json:"v83,omitempty" hbuf:"83"`   //
	V84  map[hbuf.Int64]*string          `json:"v84,omitempty" hbuf:"84"`   //
	V85  map[hbuf.Int64][]byte           `json:"v85,omitempty" hbuf:"85"`   //
	V86  map[hbuf.Int64]*[]byte          `json:"v86,omitempty" hbuf:"86"`   //
	V87  map[hbuf.Int64]float32          `json:"v87,omitempty" hbuf:"87"`   //
	V88  map[hbuf.Int64]*float32         `json:"v88,omitempty" hbuf:"88"`   //
	V89  map[hbuf.Int64]float64          `json:"v89,omitempty" hbuf:"89"`   //
	V90  map[hbuf.Int64]*float64         `json:"v90,omitempty" hbuf:"90"`   //
	V91  map[hbuf.Int64]hbuf.Time        `json:"v91,omitempty" hbuf:"91"`   //
	V92  map[hbuf.Int64]*hbuf.Time       `json:"v92,omitempty" hbuf:"92"`   //
	V93  map[hbuf.Int64]decimal.Decimal  `json:"v93,omitempty" hbuf:"93"`   //
	V94  map[hbuf.Int64]*decimal.Decimal `json:"v94,omitempty" hbuf:"94"`   //
	V95  map[hbuf.Int64]HBufSubTest      `json:"v95,omitempty" hbuf:"95"`   //
	V96  map[hbuf.Int64]*HBufSubTest     `json:"v96,omitempty" hbuf:"96"`   //
	V97  map[string]int8                 `json:"v97,omitempty" hbuf:"97"`   //
	V98  map[string]*int8                `json:"v98,omitempty" hbuf:"98"`   //
	V99  map[string]int16                `json:"v99,omitempty" hbuf:"99"`   //
	V100 map[string]*int16               `json:"v100,omitempty" hbuf:"100"` //
	V101 map[string]int32                `json:"v101,omitempty" hbuf:"101"` //
	V102 map[string]*int32               `json:"v102,omitempty" hbuf:"102"` //
	V103 map[string]hbuf.Int64           `json:"v103,omitempty" hbuf:"103"` //
	V104 map[string]*hbuf.Int64          `json:"v104,omitempty" hbuf:"104"` //
	V105 map[string]uint8                `json:"v105,omitempty" hbuf:"105"` //
	V106 map[string]*uint8               `json:"v106,omitempty" hbuf:"106"` //
	V107 map[string]uint16               `json:"v107,omitempty" hbuf:"107"` //
	V108 map[string]*uint16              `json:"v108,omitempty" hbuf:"108"` //
	V109 map[string]uint32               `json:"v109,omitempty" hbuf:"109"` //
	V110 map[string]*uint32              `json:"v110,omitempty" hbuf:"110"` //
	V111 map[string]hbuf.Uint64          `json:"v111,omitempty" hbuf:"111"` //
	V112 map[string]*hbuf.Uint64         `json:"v112,omitempty" hbuf:"112"` //
	V113 map[string]bool                 `json:"v113,omitempty" hbuf:"113"` //
	V114 map[string]*bool                `json:"v114,omitempty" hbuf:"114"` //
	V115 map[string]string               `json:"v115,omitempty" hbuf:"115"` //
	V116 map[string]*string              `json:"v116,omitempty" hbuf:"116"` //
	V117 map[string][]byte               `json:"v117,omitempty" hbuf:"117"` //
	V118 map[string]*[]byte              `json:"v118,omitempty" hbuf:"118"` //
	V119 map[string]float32              `json:"v119,omitempty" hbuf:"119"` //
	V120 map[string]*float32             `json:"v120,omitempty" hbuf:"120"` //
	V121 map[string]float64              `json:"v121,omitempty" hbuf:"121"` //
	V122 map[string]*float64             `json:"v122,omitempty" hbuf:"122"` //
	V123 map[string]hbuf.Time            `json:"v123,omitempty" hbuf:"123"` //
	V124 map[string]*hbuf.Time           `json:"v124,omitempty" hbuf:"124"` //
	V125 map[string]decimal.Decimal      `json:"v125,omitempty" hbuf:"125"` //
	V126 map[string]*decimal.Decimal     `json:"v126,omitempty" hbuf:"126"` //
	V127 map[string]HBufSubTest          `json:"v127,omitempty" hbuf:"127"` //
	V128 map[string]*HBufSubTest         `json:"v128,omitempty" hbuf:"128"` //
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

func (g *HBufTest) GetV33() []int8 {
	return g.V33
}

func (g *HBufTest) SetV33(val []int8) {
	g.V33 = val
}

func (g *HBufTest) GetV34() []*int8 {
	return g.V34
}

func (g *HBufTest) SetV34(val []*int8) {
	g.V34 = val
}

func (g *HBufTest) GetV35() []int16 {
	return g.V35
}

func (g *HBufTest) SetV35(val []int16) {
	g.V35 = val
}

func (g *HBufTest) GetV36() []*int16 {
	return g.V36
}

func (g *HBufTest) SetV36(val []*int16) {
	g.V36 = val
}

func (g *HBufTest) GetV37() []int32 {
	return g.V37
}

func (g *HBufTest) SetV37(val []int32) {
	g.V37 = val
}

func (g *HBufTest) GetV38() []*int32 {
	return g.V38
}

func (g *HBufTest) SetV38(val []*int32) {
	g.V38 = val
}

func (g *HBufTest) GetV39() []hbuf.Int64 {
	return g.V39
}

func (g *HBufTest) SetV39(val []hbuf.Int64) {
	g.V39 = val
}

func (g *HBufTest) GetV40() []*hbuf.Int64 {
	return g.V40
}

func (g *HBufTest) SetV40(val []*hbuf.Int64) {
	g.V40 = val
}

func (g *HBufTest) GetV41() []uint8 {
	return g.V41
}

func (g *HBufTest) SetV41(val []uint8) {
	g.V41 = val
}

func (g *HBufTest) GetV42() []*uint8 {
	return g.V42
}

func (g *HBufTest) SetV42(val []*uint8) {
	g.V42 = val
}

func (g *HBufTest) GetV43() []uint16 {
	return g.V43
}

func (g *HBufTest) SetV43(val []uint16) {
	g.V43 = val
}

func (g *HBufTest) GetV44() []*uint16 {
	return g.V44
}

func (g *HBufTest) SetV44(val []*uint16) {
	g.V44 = val
}

func (g *HBufTest) GetV45() []uint32 {
	return g.V45
}

func (g *HBufTest) SetV45(val []uint32) {
	g.V45 = val
}

func (g *HBufTest) GetV46() []*uint32 {
	return g.V46
}

func (g *HBufTest) SetV46(val []*uint32) {
	g.V46 = val
}

func (g *HBufTest) GetV47() []hbuf.Uint64 {
	return g.V47
}

func (g *HBufTest) SetV47(val []hbuf.Uint64) {
	g.V47 = val
}

func (g *HBufTest) GetV48() []*hbuf.Uint64 {
	return g.V48
}

func (g *HBufTest) SetV48(val []*hbuf.Uint64) {
	g.V48 = val
}

func (g *HBufTest) GetV49() []bool {
	return g.V49
}

func (g *HBufTest) SetV49(val []bool) {
	g.V49 = val
}

func (g *HBufTest) GetV50() []*bool {
	return g.V50
}

func (g *HBufTest) SetV50(val []*bool) {
	g.V50 = val
}

func (g *HBufTest) GetV51() []string {
	return g.V51
}

func (g *HBufTest) SetV51(val []string) {
	g.V51 = val
}

func (g *HBufTest) GetV52() []*string {
	return g.V52
}

func (g *HBufTest) SetV52(val []*string) {
	g.V52 = val
}

func (g *HBufTest) GetV53() [][]byte {
	return g.V53
}

func (g *HBufTest) SetV53(val [][]byte) {
	g.V53 = val
}

func (g *HBufTest) GetV54() []*[]byte {
	return g.V54
}

func (g *HBufTest) SetV54(val []*[]byte) {
	g.V54 = val
}

func (g *HBufTest) GetV55() []float32 {
	return g.V55
}

func (g *HBufTest) SetV55(val []float32) {
	g.V55 = val
}

func (g *HBufTest) GetV56() []*float32 {
	return g.V56
}

func (g *HBufTest) SetV56(val []*float32) {
	g.V56 = val
}

func (g *HBufTest) GetV57() []float64 {
	return g.V57
}

func (g *HBufTest) SetV57(val []float64) {
	g.V57 = val
}

func (g *HBufTest) GetV58() []*float64 {
	return g.V58
}

func (g *HBufTest) SetV58(val []*float64) {
	g.V58 = val
}

func (g *HBufTest) GetV59() []hbuf.Time {
	return g.V59
}

func (g *HBufTest) SetV59(val []hbuf.Time) {
	g.V59 = val
}

func (g *HBufTest) GetV60() []*hbuf.Time {
	return g.V60
}

func (g *HBufTest) SetV60(val []*hbuf.Time) {
	g.V60 = val
}

func (g *HBufTest) GetV61() []decimal.Decimal {
	return g.V61
}

func (g *HBufTest) SetV61(val []decimal.Decimal) {
	g.V61 = val
}

func (g *HBufTest) GetV62() []*decimal.Decimal {
	return g.V62
}

func (g *HBufTest) SetV62(val []*decimal.Decimal) {
	g.V62 = val
}

func (g *HBufTest) GetV63() []HBufSubTest {
	return g.V63
}

func (g *HBufTest) SetV63(val []HBufSubTest) {
	g.V63 = val
}

func (g *HBufTest) GetV64() []*HBufSubTest {
	return g.V64
}

func (g *HBufTest) SetV64(val []*HBufSubTest) {
	g.V64 = val
}

func (g *HBufTest) GetV65() map[hbuf.Int64]int8 {
	return g.V65
}

func (g *HBufTest) SetV65(val map[hbuf.Int64]int8) {
	g.V65 = val
}

func (g *HBufTest) GetV66() map[hbuf.Int64]*int8 {
	return g.V66
}

func (g *HBufTest) SetV66(val map[hbuf.Int64]*int8) {
	g.V66 = val
}

func (g *HBufTest) GetV67() map[hbuf.Int64]int16 {
	return g.V67
}

func (g *HBufTest) SetV67(val map[hbuf.Int64]int16) {
	g.V67 = val
}

func (g *HBufTest) GetV68() map[hbuf.Int64]*int16 {
	return g.V68
}

func (g *HBufTest) SetV68(val map[hbuf.Int64]*int16) {
	g.V68 = val
}

func (g *HBufTest) GetV69() map[hbuf.Int64]int32 {
	return g.V69
}

func (g *HBufTest) SetV69(val map[hbuf.Int64]int32) {
	g.V69 = val
}

func (g *HBufTest) GetV70() map[hbuf.Int64]*int32 {
	return g.V70
}

func (g *HBufTest) SetV70(val map[hbuf.Int64]*int32) {
	g.V70 = val
}

func (g *HBufTest) GetV71() map[hbuf.Int64]hbuf.Int64 {
	return g.V71
}

func (g *HBufTest) SetV71(val map[hbuf.Int64]hbuf.Int64) {
	g.V71 = val
}

func (g *HBufTest) GetV72() map[hbuf.Int64]*hbuf.Int64 {
	return g.V72
}

func (g *HBufTest) SetV72(val map[hbuf.Int64]*hbuf.Int64) {
	g.V72 = val
}

func (g *HBufTest) GetV73() map[hbuf.Int64]uint8 {
	return g.V73
}

func (g *HBufTest) SetV73(val map[hbuf.Int64]uint8) {
	g.V73 = val
}

func (g *HBufTest) GetV74() map[hbuf.Int64]*uint8 {
	return g.V74
}

func (g *HBufTest) SetV74(val map[hbuf.Int64]*uint8) {
	g.V74 = val
}

func (g *HBufTest) GetV75() map[hbuf.Int64]uint16 {
	return g.V75
}

func (g *HBufTest) SetV75(val map[hbuf.Int64]uint16) {
	g.V75 = val
}

func (g *HBufTest) GetV76() map[hbuf.Int64]*uint16 {
	return g.V76
}

func (g *HBufTest) SetV76(val map[hbuf.Int64]*uint16) {
	g.V76 = val
}

func (g *HBufTest) GetV77() map[hbuf.Int64]uint32 {
	return g.V77
}

func (g *HBufTest) SetV77(val map[hbuf.Int64]uint32) {
	g.V77 = val
}

func (g *HBufTest) GetV78() map[hbuf.Int64]*uint32 {
	return g.V78
}

func (g *HBufTest) SetV78(val map[hbuf.Int64]*uint32) {
	g.V78 = val
}

func (g *HBufTest) GetV79() map[hbuf.Int64]hbuf.Uint64 {
	return g.V79
}

func (g *HBufTest) SetV79(val map[hbuf.Int64]hbuf.Uint64) {
	g.V79 = val
}

func (g *HBufTest) GetV80() map[hbuf.Int64]*hbuf.Uint64 {
	return g.V80
}

func (g *HBufTest) SetV80(val map[hbuf.Int64]*hbuf.Uint64) {
	g.V80 = val
}

func (g *HBufTest) GetV81() map[hbuf.Int64]bool {
	return g.V81
}

func (g *HBufTest) SetV81(val map[hbuf.Int64]bool) {
	g.V81 = val
}

func (g *HBufTest) GetV82() map[hbuf.Int64]*bool {
	return g.V82
}

func (g *HBufTest) SetV82(val map[hbuf.Int64]*bool) {
	g.V82 = val
}

func (g *HBufTest) GetV83() map[hbuf.Int64]string {
	return g.V83
}

func (g *HBufTest) SetV83(val map[hbuf.Int64]string) {
	g.V83 = val
}

func (g *HBufTest) GetV84() map[hbuf.Int64]*string {
	return g.V84
}

func (g *HBufTest) SetV84(val map[hbuf.Int64]*string) {
	g.V84 = val
}

func (g *HBufTest) GetV85() map[hbuf.Int64][]byte {
	return g.V85
}

func (g *HBufTest) SetV85(val map[hbuf.Int64][]byte) {
	g.V85 = val
}

func (g *HBufTest) GetV86() map[hbuf.Int64]*[]byte {
	return g.V86
}

func (g *HBufTest) SetV86(val map[hbuf.Int64]*[]byte) {
	g.V86 = val
}

func (g *HBufTest) GetV87() map[hbuf.Int64]float32 {
	return g.V87
}

func (g *HBufTest) SetV87(val map[hbuf.Int64]float32) {
	g.V87 = val
}

func (g *HBufTest) GetV88() map[hbuf.Int64]*float32 {
	return g.V88
}

func (g *HBufTest) SetV88(val map[hbuf.Int64]*float32) {
	g.V88 = val
}

func (g *HBufTest) GetV89() map[hbuf.Int64]float64 {
	return g.V89
}

func (g *HBufTest) SetV89(val map[hbuf.Int64]float64) {
	g.V89 = val
}

func (g *HBufTest) GetV90() map[hbuf.Int64]*float64 {
	return g.V90
}

func (g *HBufTest) SetV90(val map[hbuf.Int64]*float64) {
	g.V90 = val
}

func (g *HBufTest) GetV91() map[hbuf.Int64]hbuf.Time {
	return g.V91
}

func (g *HBufTest) SetV91(val map[hbuf.Int64]hbuf.Time) {
	g.V91 = val
}

func (g *HBufTest) GetV92() map[hbuf.Int64]*hbuf.Time {
	return g.V92
}

func (g *HBufTest) SetV92(val map[hbuf.Int64]*hbuf.Time) {
	g.V92 = val
}

func (g *HBufTest) GetV93() map[hbuf.Int64]decimal.Decimal {
	return g.V93
}

func (g *HBufTest) SetV93(val map[hbuf.Int64]decimal.Decimal) {
	g.V93 = val
}

func (g *HBufTest) GetV94() map[hbuf.Int64]*decimal.Decimal {
	return g.V94
}

func (g *HBufTest) SetV94(val map[hbuf.Int64]*decimal.Decimal) {
	g.V94 = val
}

func (g *HBufTest) GetV95() map[hbuf.Int64]HBufSubTest {
	return g.V95
}

func (g *HBufTest) SetV95(val map[hbuf.Int64]HBufSubTest) {
	g.V95 = val
}

func (g *HBufTest) GetV96() map[hbuf.Int64]*HBufSubTest {
	return g.V96
}

func (g *HBufTest) SetV96(val map[hbuf.Int64]*HBufSubTest) {
	g.V96 = val
}

func (g *HBufTest) GetV97() map[string]int8 {
	return g.V97
}

func (g *HBufTest) SetV97(val map[string]int8) {
	g.V97 = val
}

func (g *HBufTest) GetV98() map[string]*int8 {
	return g.V98
}

func (g *HBufTest) SetV98(val map[string]*int8) {
	g.V98 = val
}

func (g *HBufTest) GetV99() map[string]int16 {
	return g.V99
}

func (g *HBufTest) SetV99(val map[string]int16) {
	g.V99 = val
}

func (g *HBufTest) GetV100() map[string]*int16 {
	return g.V100
}

func (g *HBufTest) SetV100(val map[string]*int16) {
	g.V100 = val
}

func (g *HBufTest) GetV101() map[string]int32 {
	return g.V101
}

func (g *HBufTest) SetV101(val map[string]int32) {
	g.V101 = val
}

func (g *HBufTest) GetV102() map[string]*int32 {
	return g.V102
}

func (g *HBufTest) SetV102(val map[string]*int32) {
	g.V102 = val
}

func (g *HBufTest) GetV103() map[string]hbuf.Int64 {
	return g.V103
}

func (g *HBufTest) SetV103(val map[string]hbuf.Int64) {
	g.V103 = val
}

func (g *HBufTest) GetV104() map[string]*hbuf.Int64 {
	return g.V104
}

func (g *HBufTest) SetV104(val map[string]*hbuf.Int64) {
	g.V104 = val
}

func (g *HBufTest) GetV105() map[string]uint8 {
	return g.V105
}

func (g *HBufTest) SetV105(val map[string]uint8) {
	g.V105 = val
}

func (g *HBufTest) GetV106() map[string]*uint8 {
	return g.V106
}

func (g *HBufTest) SetV106(val map[string]*uint8) {
	g.V106 = val
}

func (g *HBufTest) GetV107() map[string]uint16 {
	return g.V107
}

func (g *HBufTest) SetV107(val map[string]uint16) {
	g.V107 = val
}

func (g *HBufTest) GetV108() map[string]*uint16 {
	return g.V108
}

func (g *HBufTest) SetV108(val map[string]*uint16) {
	g.V108 = val
}

func (g *HBufTest) GetV109() map[string]uint32 {
	return g.V109
}

func (g *HBufTest) SetV109(val map[string]uint32) {
	g.V109 = val
}

func (g *HBufTest) GetV110() map[string]*uint32 {
	return g.V110
}

func (g *HBufTest) SetV110(val map[string]*uint32) {
	g.V110 = val
}

func (g *HBufTest) GetV111() map[string]hbuf.Uint64 {
	return g.V111
}

func (g *HBufTest) SetV111(val map[string]hbuf.Uint64) {
	g.V111 = val
}

func (g *HBufTest) GetV112() map[string]*hbuf.Uint64 {
	return g.V112
}

func (g *HBufTest) SetV112(val map[string]*hbuf.Uint64) {
	g.V112 = val
}

func (g *HBufTest) GetV113() map[string]bool {
	return g.V113
}

func (g *HBufTest) SetV113(val map[string]bool) {
	g.V113 = val
}

func (g *HBufTest) GetV114() map[string]*bool {
	return g.V114
}

func (g *HBufTest) SetV114(val map[string]*bool) {
	g.V114 = val
}

func (g *HBufTest) GetV115() map[string]string {
	return g.V115
}

func (g *HBufTest) SetV115(val map[string]string) {
	g.V115 = val
}

func (g *HBufTest) GetV116() map[string]*string {
	return g.V116
}

func (g *HBufTest) SetV116(val map[string]*string) {
	g.V116 = val
}

func (g *HBufTest) GetV117() map[string][]byte {
	return g.V117
}

func (g *HBufTest) SetV117(val map[string][]byte) {
	g.V117 = val
}

func (g *HBufTest) GetV118() map[string]*[]byte {
	return g.V118
}

func (g *HBufTest) SetV118(val map[string]*[]byte) {
	g.V118 = val
}

func (g *HBufTest) GetV119() map[string]float32 {
	return g.V119
}

func (g *HBufTest) SetV119(val map[string]float32) {
	g.V119 = val
}

func (g *HBufTest) GetV120() map[string]*float32 {
	return g.V120
}

func (g *HBufTest) SetV120(val map[string]*float32) {
	g.V120 = val
}

func (g *HBufTest) GetV121() map[string]float64 {
	return g.V121
}

func (g *HBufTest) SetV121(val map[string]float64) {
	g.V121 = val
}

func (g *HBufTest) GetV122() map[string]*float64 {
	return g.V122
}

func (g *HBufTest) SetV122(val map[string]*float64) {
	g.V122 = val
}

func (g *HBufTest) GetV123() map[string]hbuf.Time {
	return g.V123
}

func (g *HBufTest) SetV123(val map[string]hbuf.Time) {
	g.V123 = val
}

func (g *HBufTest) GetV124() map[string]*hbuf.Time {
	return g.V124
}

func (g *HBufTest) SetV124(val map[string]*hbuf.Time) {
	g.V124 = val
}

func (g *HBufTest) GetV125() map[string]decimal.Decimal {
	return g.V125
}

func (g *HBufTest) SetV125(val map[string]decimal.Decimal) {
	g.V125 = val
}

func (g *HBufTest) GetV126() map[string]*decimal.Decimal {
	return g.V126
}

func (g *HBufTest) SetV126(val map[string]*decimal.Decimal) {
	g.V126 = val
}

func (g *HBufTest) GetV127() map[string]HBufSubTest {
	return g.V127
}

func (g *HBufTest) SetV127(val map[string]HBufSubTest) {
	g.V127 = val
}

func (g *HBufTest) GetV128() map[string]*HBufSubTest {
	return g.V128
}

func (g *HBufTest) SetV128(val map[string]*HBufSubTest) {
	g.V128 = val
}
