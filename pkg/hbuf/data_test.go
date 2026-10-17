package hbuf_test

import (
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"google.golang.org/protobuf/proto"
)

// 边界测试数据：HBuf 与 Proto 两侧共用同一组值，保证体积 / 性能对比公平
var (
	testTime = time.Unix(1234567890, 987654321) // 常规时间（含纳秒）
	// JSON 可表达的时间上限：4 位年份 9999 + 纳秒上限 999999999（秒数 253402300799 覆盖 5 字节变长长度编码）
	maxTime  = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.Local)
	longStr  = strings.Repeat("Hello,世界! HBuf/proto 对比测试 ", 8) // 304 字节长字符串（覆盖多字节长度编码）
	bigDec   = decimal.RequireFromString("123456789012345678901234567890.123456789")
	smallDec = decimal.RequireFromString("-0.0000000000000000000000000001")
)

// newFullBytes 生成 0x00~0xFF 全字节值序列
func newFullBytes() []byte {
	buf := make([]byte, 256)
	for i := range buf {
		buf[i] = byte(i)
	}
	return buf
}

var fullBytes = newFullBytes() // 覆盖全部字节内容与 2 字节长度编码

var src = HBufTest{
	V1:  math.MaxInt8,
	V2:  new(int8(math.MinInt8)),
	V3:  math.MaxInt16,
	V4:  new(int16(math.MinInt16)),
	V5:  math.MaxInt32,
	V6:  new(int32(math.MinInt32)),
	V7:  math.MaxInt64,
	V8:  new(int64(math.MinInt64)),
	V9:  math.MaxUint8,
	V10: new(uint8(math.MaxUint8)),
	V11: math.MaxUint16,
	V12: new(uint16(math.MaxUint16)),
	V13: math.MaxUint32,
	V14: new(uint32(math.MaxUint32)),
	V15: math.MaxUint64,
	V16: new(uint64(math.MaxUint64)),
	V17: true,
	V18: new(false), // 非 nil 的 false 指针也写入
	V19: longStr,
	V20: new(""), // 空串指针（长度 0）
	V21: fullBytes,
	V22: new([]byte{}), // 空 bytes 指针（长度 0）
	V23: math.MaxFloat32,
	V24: new(float32(-math.MaxFloat32)),
	V25: math.MaxFloat64,
	V26: new(float64(-math.MaxFloat64)),
	V27: maxTime,
	V28: new(testTime),
	V29: bigDec,
	V30: new(smallDec),
	V31: HBufSubTest{A1: math.MaxInt32, A2: new(int64(math.MinInt32))},
	V32: &HBufSubTest{A1: math.MinInt32, A2: new(int64(math.MaxInt32))},
	// 列表：覆盖各类型最小 / 零 / 最大值（零值元素以 idLen=0 标记编码）
	V33: []int8{math.MinInt8, -1, 0, 1, math.MaxInt8},
	V34: []*int8{new(int8(math.MinInt8)), new(int8(math.MaxInt8))},
	V35: []int16{math.MinInt16, -1, 0, 1, math.MaxInt16},
	V36: []*int16{new(int16(math.MinInt16)), new(int16(math.MaxInt16))},
	V37: []int32{math.MinInt32, -1, 0, 1, math.MaxInt32},
	V38: []*int32{new(int32(math.MinInt32)), new(int32(math.MaxInt32))},
	V39: []int64{math.MinInt64, -1, 0, 1, math.MaxInt64},
	V40: []*int64{new(int64(math.MinInt64)), new(int64(math.MaxInt64))},
	V41: []uint8{0, 1, 127, 128, math.MaxUint8},
	V42: []*uint8{new(uint8(1)), new(uint8(math.MaxUint8))},
	V43: []uint16{0, 1, 255, 256, math.MaxUint16},
	V44: []*uint16{new(uint16(1)), new(uint16(math.MaxUint16))},
	V45: []uint32{0, 1, math.MaxUint32},
	V46: []*uint32{new(uint32(1)), new(uint32(math.MaxUint32))},
	V47: []uint64{0, 1, math.MaxUint64},
	V48: []*uint64{new(uint64(1)), new(uint64(math.MaxUint64))},
	V49: []bool{true, false},
	V50: []*bool{new(true), new(false)},
	V51: []string{"", "hello world", longStr},
	V52: []*string{new("hello world"), new(longStr)},
	V53: [][]byte{{0}, fullBytes},
	V54: []*[]byte{new([]byte{0}), new(fullBytes)},
	V55: []float32{-math.MaxFloat32, -1.5, 0, 1.5, math.MaxFloat32},
	V56: []*float32{new(float32(-math.MaxFloat32)), new(float32(math.MaxFloat32))},
	V57: []float64{-math.MaxFloat64, -1.5, 0, 1.5, math.MaxFloat64},
	V58: []*float64{new(float64(-math.MaxFloat64)), new(float64(math.MaxFloat64))},
	V59: []time.Time{time.Time{}, testTime, maxTime},
	V60: []*time.Time{new(testTime), new(maxTime)},
	V61: []decimal.Decimal{decimal.Zero, smallDec, bigDec},
	V62: []*decimal.Decimal{new(smallDec), new(bigDec)},
	V63: []HBufSubTest{{}, {A1: math.MaxInt32, A2: new(int64(math.MinInt32))}, {A1: math.MinInt32, A2: new(int64(math.MaxInt32))}},
	V64: []*HBufSubTest{{}, {A1: math.MaxInt32, A2: new(int64(math.MinInt32))}},
	// int64 key 的 map：key 覆盖 MinInt64 / -1 / 0 / 1 / MaxInt64，value 覆盖对应类型边界
	V65: map[int64]int8{math.MinInt64: math.MinInt8, -1: -1, 0: 0, 1: math.MaxInt8, math.MaxInt64: math.MinInt8},
	V66: map[int64]*int8{math.MinInt64: new(int8(math.MinInt8)), 1: new(int8(math.MaxInt8))},
	V67: map[int64]int16{math.MinInt64: math.MinInt16, -1: -1, 0: 0, 1: math.MaxInt16, math.MaxInt64: math.MinInt16},
	V68: map[int64]*int16{math.MinInt64: new(int16(math.MinInt16)), 1: new(int16(math.MaxInt16))},
	V69: map[int64]int32{math.MinInt64: math.MinInt32, -1: -1, 0: 0, 1: math.MaxInt32, math.MaxInt64: math.MinInt32},
	V70: map[int64]*int32{math.MinInt64: new(int32(math.MinInt32)), 1: new(int32(math.MaxInt32))},
	V71: map[int64]int64{math.MinInt64: math.MinInt64, -1: -1, 0: 0, 1: math.MaxInt64, math.MaxInt64: math.MaxInt64},
	V72: map[int64]*int64{math.MinInt64: new(int64(math.MinInt64)), 1: new(int64(math.MaxInt64))},
	V73: map[int64]uint8{math.MinInt64: 0, -1: 1, 0: 0, 1: math.MaxUint8, math.MaxInt64: math.MaxUint8},
	V74: map[int64]*uint8{math.MinInt64: new(uint8(1)), 1: new(uint8(math.MaxUint8))},
	V75: map[int64]uint16{math.MinInt64: 0, -1: 1, 0: 0, 1: math.MaxUint16, math.MaxInt64: math.MaxUint16},
	V76: map[int64]*uint16{math.MinInt64: new(uint16(1)), 1: new(uint16(math.MaxUint16))},
	V77: map[int64]uint32{math.MinInt64: 0, -1: 1, 0: 0, 1: math.MaxUint32, math.MaxInt64: math.MaxUint32},
	V78: map[int64]*uint32{math.MinInt64: new(uint32(1)), 1: new(uint32(math.MaxUint32))},
	V79: map[int64]uint64{math.MinInt64: 0, -1: 1, 0: 0, 1: math.MaxUint64, math.MaxInt64: math.MaxUint64},
	V80: map[int64]*uint64{math.MinInt64: new(uint64(1)), 1: new(uint64(math.MaxUint64))},
	V81: map[int64]bool{math.MinInt64: false, -1: false, 0: false, 1: true, math.MaxInt64: true},
	V82: map[int64]*bool{math.MinInt64: new(false), 0: new(false), 1: new(true), math.MaxInt64: new(true)},
	V83: map[int64]string{math.MinInt64: "", -1: "", 0: "", 1: "hello world", math.MaxInt64: longStr},
	V84: map[int64]*string{math.MinInt64: new("hello world"), 0: new(longStr), 1: new("hello world")},
	V85: map[int64][]byte{math.MinInt64: {0}, -1: {0}, 0: {0}, 1: fullBytes, math.MaxInt64: fullBytes},
	V86: map[int64]*[]byte{math.MinInt64: new([]byte{0}), 1: new(fullBytes)},
	V87: map[int64]float32{math.MinInt64: -math.MaxFloat32, -1: -1.5, 0: 0, 1: 1.5, math.MaxInt64: math.MaxFloat32},
	V88: map[int64]*float32{math.MinInt64: new(float32(-math.MaxFloat32)), 1: new(float32(math.MaxFloat32))},
	V89: map[int64]float64{math.MinInt64: -math.MaxFloat64, -1: -1.5, 0: 0, 1: 1.5, math.MaxInt64: math.MaxFloat64},
	V90: map[int64]*float64{math.MinInt64: new(float64(-math.MaxFloat64)), 1: new(float64(math.MaxFloat64))},
	V91: map[int64]time.Time{math.MinInt64: time.Time{}, -1: time.Time{}, 0: testTime, 1: maxTime, math.MaxInt64: maxTime},
	V92: map[int64]*time.Time{math.MinInt64: new(testTime), 0: new(maxTime)},
	V93: map[int64]decimal.Decimal{math.MinInt64: decimal.Zero, -1: decimal.Zero, 0: smallDec, 1: bigDec, math.MaxInt64: bigDec},
	V94: map[int64]*decimal.Decimal{math.MinInt64: new(smallDec), 0: new(bigDec)},
	V95: map[int64]HBufSubTest{math.MinInt64: {}, -1: {}, 0: {A1: math.MaxInt32, A2: new(int64(math.MinInt32))}, 1: {A1: math.MinInt32, A2: new(int64(math.MaxInt32))}, math.MaxInt64: {}},
	V96: map[int64]*HBufSubTest{math.MinInt64: {}, 0: {A1: math.MaxInt32, A2: new(int64(math.MinInt32))}, 1: {A1: math.MinInt32, A2: new(int64(math.MaxInt32))}},
	// string key 的 map：key 覆盖空串 / 单字符 / 长字符串
	V97:  map[string]int8{"": math.MinInt8, "a": 0, longStr: math.MaxInt8},
	V98:  map[string]*int8{"": new(int8(math.MinInt8)), longStr: new(int8(math.MaxInt8))},
	V99:  map[string]int16{"": math.MinInt16, "a": 0, longStr: math.MaxInt16},
	V100: map[string]*int16{"": new(int16(math.MinInt16)), longStr: new(int16(math.MaxInt16))},
	V101: map[string]int32{"": math.MinInt32, "a": 0, longStr: math.MaxInt32},
	V102: map[string]*int32{"": new(int32(math.MinInt32)), longStr: new(int32(math.MaxInt32))},
	V103: map[string]int64{"": math.MinInt64, "a": 0, longStr: math.MaxInt64},
	V104: map[string]*int64{"": new(int64(math.MinInt64)), longStr: new(int64(math.MaxInt64))},
	V105: map[string]uint8{"": 0, "a": 0, longStr: math.MaxUint8},
	V106: map[string]*uint8{"": new(uint8(1)), longStr: new(uint8(math.MaxUint8))},
	V107: map[string]uint16{"": 0, "a": 0, longStr: math.MaxUint16},
	V108: map[string]*uint16{"": new(uint16(1)), longStr: new(uint16(math.MaxUint16))},
	V109: map[string]uint32{"": 0, "a": 0, longStr: math.MaxUint32},
	V110: map[string]*uint32{"": new(uint32(1)), longStr: new(uint32(math.MaxUint32))},
	V111: map[string]uint64{"": 0, "a": 0, longStr: math.MaxUint64},
	V112: map[string]*uint64{"": new(uint64(1)), longStr: new(uint64(math.MaxUint64))},
	V113: map[string]bool{"": false, "a": false, longStr: true},
	V114: map[string]*bool{"": new(false), "a": new(false), longStr: new(true)},
	V115: map[string]string{"": "", "a": "hello world", longStr: longStr},
	V116: map[string]*string{"a": new("hello world"), longStr: new(longStr)},
	V117: map[string][]byte{"": {0}, "a": fullBytes, longStr: {0}},
	V118: map[string]*[]byte{"a": new(fullBytes), longStr: new([]byte{0})},
	V119: map[string]float32{"": -math.MaxFloat32, "a": 0, longStr: math.MaxFloat32},
	V120: map[string]*float32{"": new(float32(-math.MaxFloat32)), longStr: new(float32(math.MaxFloat32))},
	V121: map[string]float64{"": -math.MaxFloat64, "a": 0, longStr: math.MaxFloat64},
	V122: map[string]*float64{"": new(float64(-math.MaxFloat64)), longStr: new(float64(math.MaxFloat64))},
	V123: map[string]time.Time{"": time.Time{}, "a": testTime, longStr: maxTime},
	V124: map[string]*time.Time{"a": new(testTime), longStr: new(maxTime)},
	V125: map[string]decimal.Decimal{"": decimal.Zero, "a": smallDec, longStr: bigDec},
	V126: map[string]*decimal.Decimal{"a": new(smallDec), longStr: new(bigDec)},
	V127: map[string]HBufSubTest{"": {}, "a": {A1: math.MaxInt32, A2: new(int64(math.MinInt32))}, longStr: {A1: math.MinInt32, A2: new(int64(math.MaxInt32))}},
	V128: map[string]*HBufSubTest{"": {}, "a": {A1: math.MaxInt32, A2: new(int64(math.MinInt32))}, longStr: {A1: math.MinInt32, A2: new(int64(math.MaxInt32))}},
}

var pSrc = ProtoTest{
	V1:   math.MaxInt8,
	V2:   new(int32(math.MinInt8)),
	V3:   math.MaxInt16,
	V4:   new(int32(math.MinInt16)),
	V5:   math.MaxInt32,
	V6:   new(int32(math.MinInt32)),
	V7:   math.MaxInt64,
	V8:   new(int64(math.MinInt64)),
	V9:   math.MaxUint8,
	V10:  new(uint32(math.MaxUint8)),
	V11:  math.MaxUint16,
	V12:  new(uint32(math.MaxUint16)),
	V13:  math.MaxUint32,
	V14:  new(uint32(math.MaxUint32)),
	V15:  math.MaxUint64,
	V16:  new(uint64(math.MaxUint64)),
	V17:  true,
	V18:  new(false),
	V19:  longStr,
	V20:  new(""),
	V21:  fullBytes,
	V22:  []byte{},
	V23:  math.MaxFloat32,
	V24:  new(float32(-math.MaxFloat32)),
	V25:  math.MaxFloat64,
	V26:  new(float64(-math.MaxFloat64)),
	V27:  uint64(maxTime.UnixMicro()),
	V28:  new(uint64(testTime.UnixMicro())),
	V29:  bigDec.String(),
	V30:  smallDec.String(),
	V31:  &ProtoSubTest{V1: math.MaxInt32, V2: math.MinInt32},
	V32:  &ProtoSubTest{V1: math.MinInt32, V2: math.MaxInt32},
	V33:  []int32{math.MinInt8, -1, 0, 1, math.MaxInt8},
	V34:  []int32{math.MinInt8, math.MaxInt8},
	V35:  []int32{math.MinInt16, -1, 0, 1, math.MaxInt16},
	V36:  []int32{math.MinInt16, math.MaxInt16},
	V37:  []int32{math.MinInt32, -1, 0, 1, math.MaxInt32},
	V38:  []int32{math.MinInt32, math.MaxInt32},
	V39:  []int64{math.MinInt64, -1, 0, 1, math.MaxInt64},
	V40:  []int64{math.MinInt64, math.MaxInt64},
	V41:  []uint32{0, 1, 127, 128, math.MaxUint8},
	V42:  []uint32{1, math.MaxUint8},
	V43:  []uint32{0, 1, 255, 256, math.MaxUint16},
	V44:  []uint32{1, math.MaxUint16},
	V45:  []uint32{0, 1, math.MaxUint32},
	V46:  []uint32{1, math.MaxUint32},
	V47:  []uint64{0, 1, math.MaxUint64},
	V48:  []uint64{1, math.MaxUint64},
	V49:  []bool{true, false},
	V50:  []bool{true, false},
	V51:  []string{"", "hello world", longStr},
	V52:  []string{"hello world", longStr},
	V53:  [][]byte{{0}, fullBytes},
	V54:  [][]byte{{0}, fullBytes},
	V55:  []float32{-math.MaxFloat32, -1.5, 0, 1.5, math.MaxFloat32},
	V56:  []float32{-math.MaxFloat32, math.MaxFloat32},
	V57:  []float64{-math.MaxFloat64, -1.5, 0, 1.5, math.MaxFloat64},
	V58:  []float64{-math.MaxFloat64, math.MaxFloat64},
	V59:  []uint64{uint64(maxTime.UnixMicro()), uint64(testTime.UnixMicro()), uint64(time.Time{}.UnixMicro())},
	V60:  []uint64{uint64(testTime.UnixMicro()), uint64(maxTime.UnixMicro())},
	V61:  []string{decimal.Zero.String(), smallDec.String(), bigDec.String()},
	V62:  []string{smallDec.String(), bigDec.String()},
	V63:  []*ProtoSubTest{{}, {V1: math.MaxInt32, V2: math.MinInt32}, {V1: math.MinInt32, V2: math.MaxInt32}},
	V64:  []*ProtoSubTest{{}, {V1: math.MaxInt32, V2: math.MinInt32}},
	V65:  map[int64]int32{math.MinInt64: math.MinInt8, -1: -1, 0: 0, 1: math.MaxInt8, math.MaxInt64: math.MinInt8},
	V66:  map[int64]int32{math.MinInt64: math.MinInt8, 1: math.MaxInt8},
	V67:  map[int64]int32{math.MinInt64: math.MinInt16, -1: -1, 0: 0, 1: math.MaxInt16, math.MaxInt64: math.MinInt16},
	V68:  map[int64]int32{math.MinInt64: math.MinInt16, 1: math.MaxInt16},
	V69:  map[int64]int32{math.MinInt64: math.MinInt32, -1: -1, 0: 0, 1: math.MaxInt32, math.MaxInt64: math.MinInt32},
	V70:  map[int64]int32{math.MinInt64: math.MinInt32, 1: math.MaxInt32},
	V71:  map[int64]int64{math.MinInt64: math.MinInt64, -1: -1, 0: 0, 1: math.MaxInt64, math.MaxInt64: math.MaxInt64},
	V72:  map[int64]int64{math.MinInt64: math.MinInt64, 1: math.MaxInt64},
	V73:  map[int64]uint32{math.MinInt64: 0, -1: 1, 0: 0, 1: math.MaxUint8, math.MaxInt64: math.MaxUint8},
	V74:  map[int64]uint32{math.MinInt64: 1, 1: math.MaxUint8},
	V75:  map[int64]uint32{math.MinInt64: 0, -1: 1, 0: 0, 1: math.MaxUint16, math.MaxInt64: math.MaxUint16},
	V76:  map[int64]uint32{math.MinInt64: 1, 1: math.MaxUint16},
	V77:  map[int64]uint32{math.MinInt64: 0, -1: 1, 0: 0, 1: math.MaxUint32, math.MaxInt64: math.MaxUint32},
	V78:  map[int64]uint32{math.MinInt64: 1, 1: math.MaxUint32},
	V79:  map[int64]uint64{math.MinInt64: 0, -1: 1, 0: 0, 1: math.MaxUint64, math.MaxInt64: math.MaxUint64},
	V80:  map[int64]uint64{math.MinInt64: 1, 1: math.MaxUint64},
	V81:  map[int64]bool{math.MinInt64: false, -1: false, 0: false, 1: true, math.MaxInt64: true},
	V82:  map[int64]bool{math.MinInt64: false, 0: false, 1: true, math.MaxInt64: true},
	V83:  map[int64]string{math.MinInt64: "", -1: "", 0: "", 1: "hello world", math.MaxInt64: longStr},
	V84:  map[int64]string{math.MinInt64: "hello world", 0: longStr, 1: "hello world"},
	V85:  map[int64][]byte{math.MinInt64: {0}, -1: {0}, 0: {0}, 1: fullBytes, math.MaxInt64: fullBytes},
	V86:  map[int64][]byte{math.MinInt64: {0}, 1: fullBytes},
	V87:  map[int64]float32{math.MinInt64: -math.MaxFloat32, -1: -1.5, 0: 0, 1: 1.5, math.MaxInt64: math.MaxFloat32},
	V88:  map[int64]float32{math.MinInt64: -math.MaxFloat32, 1: math.MaxFloat32},
	V89:  map[int64]float64{math.MinInt64: -math.MaxFloat64, -1: -1.5, 0: 0, 1: 1.5, math.MaxInt64: math.MaxFloat64},
	V90:  map[int64]float64{math.MinInt64: -math.MaxFloat64, 1: math.MaxFloat64},
	V91:  map[int64]uint64{math.MinInt64: uint64(time.Time{}.UnixMicro()), -1: uint64(time.Time{}.UnixMicro()), 0: uint64(testTime.UnixMicro()), 1: uint64(maxTime.UnixMicro()), math.MaxInt64: uint64(maxTime.UnixMicro())},
	V92:  map[int64]uint64{math.MinInt64: uint64(testTime.UnixMicro()), 0: uint64(maxTime.UnixMicro())},
	V93:  map[int64]string{math.MinInt64: decimal.Zero.String(), -1: decimal.Zero.String(), 0: smallDec.String(), 1: bigDec.String(), math.MaxInt64: bigDec.String()},
	V94:  map[int64]string{math.MinInt64: smallDec.String(), 0: bigDec.String()},
	V95:  map[int64]*ProtoSubTest{math.MinInt64: {}, -1: {}, 0: {V1: math.MaxInt32, V2: math.MinInt32}, 1: {V1: math.MinInt32, V2: math.MaxInt32}, math.MaxInt64: {}},
	V96:  map[int64]*ProtoSubTest{math.MinInt64: {}, 0: {V1: math.MaxInt32, V2: math.MinInt32}, 1: {V1: math.MinInt32, V2: math.MaxInt32}},
	V97:  map[string]int32{"": math.MinInt8, "a": 0, longStr: math.MaxInt8},
	V98:  map[string]int32{"": math.MinInt8, longStr: math.MaxInt8},
	V99:  map[string]int32{"": math.MinInt16, "a": 0, longStr: math.MaxInt16},
	V100: map[string]int32{"": math.MinInt16, longStr: math.MaxInt16},
	V101: map[string]int32{"": math.MinInt32, "a": 0, longStr: math.MaxInt32},
	V102: map[string]int32{"": math.MinInt32, longStr: math.MaxInt32},
	V103: map[string]int64{"": math.MinInt64, "a": 0, longStr: math.MaxInt64},
	V104: map[string]int64{"": math.MinInt64, longStr: math.MaxInt64},
	V105: map[string]uint32{"": 0, "a": 0, longStr: math.MaxUint8},
	V106: map[string]uint32{"": 1, longStr: math.MaxUint8},
	V107: map[string]uint32{"": 0, "a": 0, longStr: math.MaxUint16},
	V108: map[string]uint32{"": 1, longStr: math.MaxUint16},
	V109: map[string]uint32{"": 0, "a": 0, longStr: math.MaxUint32},
	V110: map[string]uint32{"": 1, longStr: math.MaxUint32},
	V111: map[string]uint64{"": 0, "a": 0, longStr: math.MaxUint64},
	V112: map[string]uint64{"": 1, longStr: math.MaxUint64},
	V113: map[string]bool{"": false, "a": false, longStr: true},
	V114: map[string]bool{"": false, "a": false, longStr: true},
	V115: map[string]string{"": "", "a": "hello world", longStr: longStr},
	V116: map[string]string{"a": "hello world", longStr: longStr},
	V117: map[string][]byte{"": {0}, "a": fullBytes, longStr: {0}},
	V118: map[string][]byte{"a": fullBytes, longStr: {0}},
	V119: map[string]float32{"": -math.MaxFloat32, "a": 0, longStr: math.MaxFloat32},
	V120: map[string]float32{"": -math.MaxFloat32, longStr: math.MaxFloat32},
	V121: map[string]float64{"": -math.MaxFloat64, "a": 0, longStr: math.MaxFloat64},
	V122: map[string]float64{"": -math.MaxFloat64, longStr: math.MaxFloat64},
	V123: map[string]uint64{"": uint64(time.Time{}.UnixMicro()), "a": uint64(testTime.UnixMicro()), longStr: uint64(maxTime.UnixMicro())},
	V124: map[string]uint64{"a": uint64(testTime.UnixMicro()), longStr: uint64(maxTime.UnixMicro())},
	V125: map[string]string{"": decimal.Zero.String(), "a": smallDec.String(), longStr: bigDec.String()},
	V126: map[string]string{"a": smallDec.String(), longStr: bigDec.String()},
	V127: map[string]*ProtoSubTest{"": {}, "a": {V1: math.MaxInt32, V2: math.MinInt32}, longStr: {V1: math.MinInt32, V2: math.MaxInt32}},
	V128: map[string]*ProtoSubTest{"": {}, "a": {V1: math.MaxInt32, V2: math.MinInt32}, longStr: {V1: math.MinInt32, V2: math.MaxInt32}},
}

func TestEncoderDecoder(t *testing.T) {
	var err error
	var pBuf []byte
	t.Run("EncoderProto", func(t *testing.T) {
		pBuf, err = proto.Marshal(&pSrc)
		if err != nil {
			t.Error(err.Error())
			return
		}
		t.Log("EncoderProto len:", len(pBuf))
	})

	jText := ""
	t.Run("EncoderJson", func(t *testing.T) {
		buf, err := json.Marshal(&src)
		if err != nil {
			t.Error(err.Error() + "\n" + string(buf))
			return
		}
		t.Log("EncoderJson len:", len(buf))
		jText = string(buf)
	})

	var hBuf []byte
	t.Run("EncoderHBuf", func(t *testing.T) {
		hBuf, err = hbuf.Marshal(&src)
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
		t.Log("EncoderHBuf len:", len(hBuf))
		err = os.WriteFile(filepath.Join(os.TempDir(), "test.bin"), hBuf, 0666)
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
	})

	hText := ""
	des := HBufTest{}
	t.Run("DecoderHBuf", func(t *testing.T) {
		hBuf, err = os.ReadFile(filepath.Join(os.TempDir(), "test.bin"))
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
		des = HBufTest{}
		err = hbuf.Unmarshal(hBuf, &des)
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
		buf, _ := json.Marshal(&des)
		hText = string(buf)
	})
	// json/v2 输出不固定 map 键序，改用 v1（map 键有序）做全量比对
	jTextV1, _ := jsonv1.Marshal(&src)
	hTextV1, _ := jsonv1.Marshal(&des)
	if string(jTextV1) != string(hTextV1) {
		t.Error("jText != hText")
	}
	t.Log("src:", jText)
	t.Log("desc:", hText)
}

func BenchmarkEncoderDecoder(b *testing.B) {
	b.ReportAllocs()

	var pBuf, jBuf, hBuf []byte

	b.Run("EncoderProto", func(b *testing.B) {
		b.ReportAllocs()
		var buf []byte
		var err error
		for i := 0; i < b.N; i++ {
			buf, err = proto.Marshal(&pSrc)
			if err != nil {
				b.Fatal(err)
			}
		}
		pBuf = buf
	})

	b.Run("DecoderProto", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			des := ProtoTest{}
			if err := proto.Unmarshal(pBuf, &des); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("EncoderJson", func(b *testing.B) {
		b.ReportAllocs()
		var buf []byte
		var err error
		for i := 0; i < b.N; i++ {
			buf, err = json.Marshal(&src) // 注意：json.Marshal 比 Encoder.Encode 更直接
			if err != nil {
				b.Fatal(err)
			}
		}
		jBuf = buf
	})

	b.Run("DecoderJson", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			des := HBufTest{}
			if err := json.Unmarshal(jBuf, &des); err != nil { // 对应 Unmarshal
				b.Fatal(err)
			}
		}
	})

	b.Run("EncoderHBuf", func(b *testing.B) {
		b.ReportAllocs()
		var buf []byte
		var err error
		for i := 0; i < b.N; i++ {
			buf, err = hbuf.Marshal(&src)
			if err != nil {
				b.Fatal(err)
			}
		}
		hBuf = buf
	})

	b.Run("DecoderHBuf", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			des := HBufTest{}
			if err := hbuf.Unmarshal(hBuf, &des); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestExtend(t *testing.T) {
	eSrc := HBufSub1Test{
		HBufTest: src,
		C1:       55,
		C2:       new(int64(66)),
		A1:       12, A2: new(int64(12)),
		V1: 44,
		V2: new(int64(33)),
	}

	buf, err := hbuf.Marshal(&eSrc)
	if err != nil {
		t.Error(err.Error() + "\n")
		return
	}
	des := HBufSub1Test{}
	err = hbuf.Unmarshal(buf, &des)
	if err != nil {
		t.Error(err.Error() + "\n")
		return
	}

	sJson, _ := jsonv1.Marshal(&eSrc)
	desJson, _ := jsonv1.Marshal(&des)

	t.Log(string(sJson))
	t.Log(string(desJson))
	if string(sJson) != string(desJson) {
		t.Error("not equal")
		return
	}
}

// TestTimeBoundary 验证 HBuf 时间编码的 42 位秒 + 30 位纳秒上限边界 round-trip。
// 该时间（公元 139240 年）超出 JSON 4 位年份范围，无法参与 TestEncoderDecoder 的公平对比，故单独验证。
func TestTimeBoundary(t *testing.T) {
	hbufMaxTime := time.Unix(4398046511103, 999999999) // 42 位秒上限（0x3FFFFFFFFFF）+ 纳秒上限
	high8Max := time.Unix(17179869183, 999999999)      // 2^34 - 1：秒高 8 位仍为 0 的最大值
	high8Bound := time.Unix(17179869184, 0)            // 2^34：秒高 8 位从 0 跃迁到 1

	srcTime := HBufTest{
		V27:  hbufMaxTime,
		V28:  &hbufMaxTime,
		V59:  []time.Time{time.Time{}, high8Max, high8Bound, hbufMaxTime},
		V60:  []*time.Time{&high8Max, &hbufMaxTime},
		V91:  map[int64]time.Time{0: hbufMaxTime, 1: time.Time{}},
		V92:  map[int64]*time.Time{0: &high8Bound, 1: &hbufMaxTime},
		V123: map[string]time.Time{"zero": time.Time{}, "max": hbufMaxTime},
	}

	buf, err := hbuf.Marshal(&srcTime)
	if err != nil {
		t.Fatal(err)
	}
	des := HBufTest{}
	if err = hbuf.Unmarshal(buf, &des); err != nil {
		t.Fatal(err)
	}

	assertTime := func(name string, got, want time.Time) {
		if !got.Equal(want) {
			t.Errorf("%s round-trip mismatch: got %v want %v", name, got, want)
		}
	}
	assertTime("V27", des.V27, hbufMaxTime)
	if des.V28 == nil {
		t.Fatal("V28 is nil")
	}
	assertTime("V28", *des.V28, hbufMaxTime)
	if len(des.V59) != 4 {
		t.Fatalf("V59 len: got %d want 4", len(des.V59))
	}
	if !des.V59[0].IsZero() {
		t.Errorf("V59[0] want zero time, got %v", des.V59[0])
	}
	assertTime("V59[1]", des.V59[1], high8Max)
	assertTime("V59[2]", des.V59[2], high8Bound)
	assertTime("V59[3]", des.V59[3], hbufMaxTime)
	if len(des.V60) != 2 || des.V60[0] == nil || des.V60[1] == nil {
		t.Fatalf("V60 invalid: %v", des.V60)
	}
	assertTime("V60[0]", *des.V60[0], high8Max)
	assertTime("V60[1]", *des.V60[1], hbufMaxTime)
	if len(des.V91) != 2 {
		t.Fatalf("V91 len: got %d want 2", len(des.V91))
	}
	assertTime("V91[0]", des.V91[0], hbufMaxTime)
	if !des.V91[1].IsZero() {
		t.Errorf("V91[1] want zero time, got %v", des.V91[1])
	}
	if len(des.V92) != 2 || des.V92[0] == nil || des.V92[1] == nil {
		t.Fatalf("V92 invalid: %v", des.V92)
	}
	assertTime("V92[0]", *des.V92[0], high8Bound)
	assertTime("V92[1]", *des.V92[1], hbufMaxTime)
	if len(des.V123) != 2 {
		t.Fatalf("V123 len: got %d want 2", len(des.V123))
	}
	if !des.V123["zero"].IsZero() {
		t.Errorf("V123[zero] want zero time, got %v", des.V123["zero"])
	}
	assertTime("V123[max]", des.V123["max"], hbufMaxTime)
}
