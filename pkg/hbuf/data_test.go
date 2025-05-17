package hbuf_test

import (
	"bytes"
	"encoding/json"
	"github.com/shopspring/decimal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"google.golang.org/protobuf/proto"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TP[T any](v T) *T {
	return &v
}

var src = HBufTest{
	V1:   12,
	V2:   TP(int8(-12)),
	V3:   1234,
	V4:   TP(int16(-1234)),
	V5:   123456,
	V6:   TP(int32(-123456)),
	V7:   123456789,
	V8:   TP(hbuf.Int64(-123456789)),
	V9:   200,
	V10:  TP(uint8(200)),
	V11:  65530,
	V12:  TP(uint16(65530)),
	V13:  655306553,
	V14:  TP(uint32(655306553)),
	V15:  6553065535,
	V16:  TP(hbuf.Uint64(6553065535)),
	V17:  true,
	V18:  TP(true),
	V19:  "hello world",
	V20:  TP("hello world"),
	V21:  []byte("hello world123456"),
	V22:  TP([]byte("hello world123456")),
	V23:  3.1415926535895781,
	V24:  TP(float32(-3.1415926535895781)),
	V25:  3.1415926535895781,
	V26:  TP(float64(-3.1415926535895781)),
	V27:  hbuf.Time(time.Now()),
	V28:  TP(hbuf.Time(time.Now())),
	V29:  decimal.NewFromFloat(3.1415926535),
	V30:  TP(decimal.NewFromFloat(-3.14159265359)),
	V31:  HBufSubTest{V1: 12, V2: TP(int8(12))},
	V32:  &HBufSubTest{V1: 45, V2: TP(int8(45))},
	V33:  []int8{1, 2, 3},
	V34:  []*int8{TP(int8(1)), TP(int8(2)), TP(int8(3))},
	V35:  []int16{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V36:  []*int16{TP(int16(1)), TP(int16(2)), TP(int16(3)), TP(int16(4)), TP(int16(5)), TP(int16(6)), TP(int16(7)), TP(int16(8)), TP(int16(9)), TP(int16(10))},
	V37:  []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V38:  []*int32{TP(int32(1)), TP(int32(2)), TP(int32(3)), TP(int32(4)), TP(int32(5)), TP(int32(6)), TP(int32(7)), TP(int32(8)), TP(int32(9)), TP(int32(10))},
	V39:  []hbuf.Int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V40:  []*hbuf.Int64{TP(hbuf.Int64(1)), TP(hbuf.Int64(2)), TP(hbuf.Int64(3)), TP(hbuf.Int64(4)), TP(hbuf.Int64(5)), TP(hbuf.Int64(6)), TP(hbuf.Int64(7)), TP(hbuf.Int64(8)), TP(hbuf.Int64(9)), TP(hbuf.Int64(10))},
	V41:  []uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V42:  []*uint8{TP(uint8(1)), TP(uint8(2)), TP(uint8(3))},
	V43:  []uint16{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V44:  []*uint16{TP(uint16(1)), TP(uint16(2)), TP(uint16(3)), TP(uint16(4)), TP(uint16(5)), TP(uint16(6)), TP(uint16(7)), TP(uint16(8)), TP(uint16(9)), TP(uint16(10))},
	V45:  []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V46:  []*uint32{TP(uint32(1)), TP(uint32(2))},
	V47:  []hbuf.Uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V48:  []*hbuf.Uint64{TP(hbuf.Uint64(1)), TP(hbuf.Uint64(2))},
	V49:  []bool{true, false},
	V50:  []*bool{TP(true), TP(false)},
	V51:  []string{"hello world", "hello world123456", "hello world123456"},
	V52:  []*string{TP("hello world"), TP("hello world123456"), TP("hello world123456")},
	V53:  [][]byte{{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
	V54:  []*[]byte{{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
	V55:  []float32{1.58817, 2.58817, 3.58817, 4.58817, -5.58817, 6.58817, -7.58817, 8.58817, 9.58817, 10.58817},
	V56:  []*float32{TP(float32(1.58817)), TP(float32(2.58817)), TP(float32(3.58817)), TP(float32(4.58817)), TP(float32(5.58817)), TP(float32(6.58817)), TP(float32(7.58817)), TP(float32(8.58817)), TP(float32(9.58817)), TP(float32(10.58817))},
	V57:  []float64{1.58817, 2.58817, 3.58817, 4.58817, 5.58817, 6.58817, 7.58817, 8.58817, 9.58817, 10.58817},
	V58:  []*float64{TP(float64(1.58817)), TP(float64(2.58817)), TP(float64(3.58817)), TP(float64(4.58817)), TP(float64(5.58817)), TP(float64(6.58817)), TP(float64(7.58817)), TP(float64(8.58817)), TP(float64(9.58817)), TP(float64(10.58817))},
	V59:  []hbuf.Time{hbuf.Time(time.Now()), hbuf.Time(time.Now()), hbuf.Time(time.Now())},
	V60:  []*hbuf.Time{TP(hbuf.Time(time.Now())), TP(hbuf.Time(time.Now())), TP(hbuf.Time(time.Now()))},
	V61:  []decimal.Decimal{decimal.NewFromFloat(3.1415926535), decimal.NewFromFloat(3.14159265359), decimal.NewFromFloat(3.14159265359)},
	V62:  []*decimal.Decimal{TP(decimal.NewFromFloat(3.1415926535)), TP(decimal.NewFromFloat(3.14159265359)), TP(decimal.NewFromFloat(3.14159265359))},
	V63:  []HBufSubTest{{V1: 12, V2: TP(int8(12))}, {V1: 45, V2: TP(int8(45))}, {V1: 78, V2: TP(int8(78))}},
	V64:  []*HBufSubTest{{V1: 12, V2: TP(int8(12))}, {V1: 45, V2: TP(int8(45))}, {V1: 78, V2: TP(int8(78))}},
	V65:  map[hbuf.Int64]int8{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V66:  map[hbuf.Int64]*int8{1: TP(int8(1)), 2: TP(int8(2)), 3: TP(int8(3)), 4: TP(int8(4)), 5: TP(int8(5)), 6: TP(int8(6)), 7: TP(int8(7)), 8: TP(int8(8)), 9: TP(int8(9)), 10: TP(int8(10))},
	V67:  map[hbuf.Int64]int16{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V68:  map[hbuf.Int64]*int16{1: TP(int16(1)), 2: TP(int16(2)), 3: TP(int16(3)), 4: TP(int16(4)), 5: TP(int16(5)), 6: TP(int16(6)), 7: TP(int16(7)), 8: TP(int16(8)), 9: TP(int16(9)), 10: TP(int16(10))},
	V69:  map[hbuf.Int64]int32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V70:  map[hbuf.Int64]*int32{1: TP(int32(1)), 2: TP(int32(2)), 3: TP(int32(3)), 4: TP(int32(4)), 5: TP(int32(5)), 6: TP(int32(6)), 7: TP(int32(7)), 8: TP(int32(8)), 9: TP(int32(9)), 10: TP(int32(10))},
	V71:  map[hbuf.Int64]hbuf.Int64{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V72:  map[hbuf.Int64]*hbuf.Int64{1: TP(hbuf.Int64(1)), 2: TP(hbuf.Int64(2)), 3: TP(hbuf.Int64(3)), 4: TP(hbuf.Int64(4)), 5: TP(hbuf.Int64(5)), 6: TP(hbuf.Int64(6)), 7: TP(hbuf.Int64(7)), 8: TP(hbuf.Int64(8)), 9: TP(hbuf.Int64(9)), 10: TP(hbuf.Int64(10))},
	V73:  map[hbuf.Int64]uint8{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V74:  map[hbuf.Int64]*uint8{1: TP(uint8(1)), 2: TP(uint8(2)), 3: TP(uint8(3)), 4: TP(uint8(4)), 5: TP(uint8(5)), 6: TP(uint8(6)), 7: TP(uint8(7)), 8: TP(uint8(8)), 9: TP(uint8(9)), 10: TP(uint8(10))},
	V75:  map[hbuf.Int64]uint16{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V76:  map[hbuf.Int64]*uint16{1: TP(uint16(1)), 2: TP(uint16(2)), 3: TP(uint16(3)), 4: TP(uint16(4)), 5: TP(uint16(5)), 6: TP(uint16(6)), 7: TP(uint16(7)), 8: TP(uint16(8)), 9: TP(uint16(9)), 10: TP(uint16(10))},
	V77:  map[hbuf.Int64]uint32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V78:  map[hbuf.Int64]*uint32{1: TP(uint32(1)), 2: TP(uint32(2)), 3: TP(uint32(3)), 4: TP(uint32(4)), 5: TP(uint32(5)), 6: TP(uint32(6)), 7: TP(uint32(7)), 8: TP(uint32(8)), 9: TP(uint32(9)), 10: TP(uint32(10))},
	V79:  map[hbuf.Int64]hbuf.Uint64{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V80:  map[hbuf.Int64]*hbuf.Uint64{1: TP(hbuf.Uint64(1)), 2: TP(hbuf.Uint64(2)), 3: TP(hbuf.Uint64(3)), 4: TP(hbuf.Uint64(4)), 5: TP(hbuf.Uint64(5)), 6: TP(hbuf.Uint64(6)), 7: TP(hbuf.Uint64(7)), 8: TP(hbuf.Uint64(8)), 9: TP(hbuf.Uint64(9)), 10: TP(hbuf.Uint64(10))},
	V81:  map[hbuf.Int64]bool{1: true, 2: false, 3: true, 4: false, 5: true, 6: false, 7: true, 8: false, 9: true, 10: false},
	V82:  map[hbuf.Int64]*bool{1: TP(true), 2: TP(false), 3: TP(true), 4: TP(false), 5: TP(true), 6: TP(false), 7: TP(true), 8: TP(false), 9: TP(true), 10: TP(false)},
	V83:  map[hbuf.Int64]string{1: "1", 2: "2", 3: "3", 4: "4", 5: "5", 6: "6", 7: "7", 8: "8", 9: "9", 10: "10"},
	V84:  map[hbuf.Int64]*string{1: TP("1"), 2: TP("2"), 3: TP("3"), 4: TP("4"), 5: TP("5"), 6: TP("6"), 7: TP("7"), 8: TP("8"), 9: TP("9"), 10: TP("10")},
	V85:  map[hbuf.Int64][]byte{1: {1, 2}, 2: {3, 4}, 3: {5, 6}, 4: {7, 8}, 5: {9, 10}},
	V86:  map[hbuf.Int64]*[]byte{1: TP([]byte{1, 2}), 2: TP([]byte{3, 4}), 3: TP([]byte{5, 6}), 4: TP([]byte{7, 8}), 5: TP([]byte{9, 10})},
	V87:  map[hbuf.Int64]float32{1: 1.1, 2: 2.2, 3: 3.3, 4: 4.4, 5: 5.5},
	V88:  map[hbuf.Int64]*float32{1: TP(float32(1.1)), 2: TP(float32(2.2)), 3: TP(float32(3.3)), 4: TP(float32(4.4)), 5: TP(float32(5.5))},
	V89:  map[hbuf.Int64]float64{1: 1.1, 2: 2.2, 3: 3.3, 4: 4.4, 5: 5.5},
	V90:  map[hbuf.Int64]*float64{1: TP(float64(1.1)), 2: TP(float64(2.2)), 3: TP(float64(3.3)), 4: TP(float64(4.4)), 5: TP(float64(5.5))},
	V91:  map[hbuf.Int64]hbuf.Time{1: hbuf.Time(time.Now()), 2: hbuf.Time(time.Now()), 3: hbuf.Time(time.Now())},
	V92:  map[hbuf.Int64]*hbuf.Time{1: TP(hbuf.Time(time.Now())), 2: TP(hbuf.Time(time.Now())), 3: TP(hbuf.Time(time.Now()))},
	V93:  map[hbuf.Int64]decimal.Decimal{1: decimal.NewFromFloat(3.1415926535), 2: decimal.NewFromFloat(3.14159265359), 3: decimal.NewFromFloat(3.14159265359)},
	V94:  map[hbuf.Int64]*decimal.Decimal{1: TP(decimal.NewFromFloat(3.1415926535)), 2: TP(decimal.NewFromFloat(3.14159265359)), 3: TP(decimal.NewFromFloat(3.14159265359))},
	V95:  map[hbuf.Int64]HBufSubTest{1: {V1: 22, V2: TP(int8(11))}, 2: {V1: 33, V2: TP(int8(22))}, 3: {V1: 44, V2: TP(int8(33))}},
	V96:  map[hbuf.Int64]*HBufSubTest{1: TP(HBufSubTest{V1: 22, V2: TP(int8(11))}), 2: TP(HBufSubTest{V1: 33, V2: TP(int8(22))}), 3: TP(HBufSubTest{V1: 44, V2: TP(int8(33))})},
	V97:  map[string]int8{"a": 1, "b": 2, "c": 3},
	V98:  map[string]*int8{"a": TP(int8(1)), "b": TP(int8(2)), "c": TP(int8(3))},
	V99:  map[string]int16{"a": 1, "b": 2, "c": 3},
	V100: map[string]*int16{"a": TP(int16(1)), "b": TP(int16(2)), "c": TP(int16(3))},
	V101: map[string]int32{"a": 1, "b": 2, "c": 3},
	V102: map[string]*int32{"a": TP(int32(1)), "b": TP(int32(2)), "c": TP(int32(3))},
	V103: map[string]hbuf.Int64{"a": 1, "b": 2, "c": 3},
	V104: map[string]*hbuf.Int64{"a": TP(hbuf.Int64(1)), "b": TP(hbuf.Int64(2)), "c": TP(hbuf.Int64(3))},
	V105: map[string]uint8{"a": 1, "b": 2, "c": 3},
	V106: map[string]*uint8{"a": TP(uint8(1)), "b": TP(uint8(2)), "c": TP(uint8(3))},
	V107: map[string]uint16{"a": 1, "b": 2, "c": 3},
	V108: map[string]*uint16{"a": TP(uint16(1)), "b": TP(uint16(2)), "c": TP(uint16(3))},
	V109: map[string]uint32{"a": 1, "b": 2, "c": 3},
	V110: map[string]*uint32{"a": TP(uint32(1)), "b": TP(uint32(2)), "c": TP(uint32(3))},
	V111: map[string]hbuf.Uint64{"a": 1, "b": 2, "c": 3},
	V112: map[string]*hbuf.Uint64{"a": TP(hbuf.Uint64(1)), "b": TP(hbuf.Uint64(2)), "c": TP(hbuf.Uint64(3))},
	V113: map[string]bool{"a": true, "b": false, "c": true},
	V114: map[string]*bool{"a": TP(true), "b": TP(false), "c": TP(true)},
	V115: map[string]string{"a": "1", "b": "2", "c": "3"},
	V116: map[string]*string{"a": TP("1"), "b": TP("2"), "c": TP("3")},
	V117: map[string][]byte{"a": {1, 2}, "b": {3, 4}, "c": {5, 6}},
	V118: map[string]*[]byte{"a": TP([]byte{1, 2}), "b": TP([]byte{3, 4}), "c": TP([]byte{5, 6})},
	V119: map[string]float32{"a": 1.1, "b": 2.2, "c": 3.3},
	V120: map[string]*float32{"a": TP(float32(1.1)), "b": TP(float32(2.2)), "c": TP(float32(3.3))},
	V121: map[string]float64{"a": 1.1, "b": 2.2, "c": 3.3},
	V122: map[string]*float64{"a": TP(float64(1.1)), "b": TP(float64(2.2)), "c": TP(float64(3.3))},
	V123: map[string]hbuf.Time{"a": hbuf.Time(time.Now()), "b": hbuf.Time(time.Now()), "c": hbuf.Time(time.Now())},
	V124: map[string]*hbuf.Time{"a": TP(hbuf.Time(time.Now())), "b": TP(hbuf.Time(time.Now())), "c": TP(hbuf.Time(time.Now()))},
	V125: map[string]decimal.Decimal{"a": decimal.NewFromFloat(3.1415926535), "b": decimal.NewFromFloat(3.14159265359), "c": decimal.NewFromFloat(3.14159265359)},
	V126: map[string]*decimal.Decimal{"a": TP(decimal.NewFromFloat(3.1415926535)), "b": TP(decimal.NewFromFloat(3.14159265359)), "c": TP(decimal.NewFromFloat(3.14159265359))},
	V127: map[string]HBufSubTest{"a": {V1: 22, V2: TP(int8(11))}, "b": {V1: 33, V2: TP(int8(22))}, "c": {V1: 44, V2: TP(int8(33))}},
	V128: map[string]*HBufSubTest{"a": TP(HBufSubTest{V1: 22, V2: TP(int8(11))}), "b": TP(HBufSubTest{V1: 33, V2: TP(int8(22))}), "c": TP(HBufSubTest{V1: 44, V2: TP(int8(33))})},
}

var pSrc = ProtoTest{
	V1:   12,
	V2:   TP(int32(-12)),
	V3:   1234,
	V4:   TP(int32(-1234)),
	V5:   123456,
	V6:   TP(int32(-123456)),
	V7:   123456789,
	V8:   TP(int64(-123456789)),
	V9:   200,
	V10:  TP(uint32(200)),
	V11:  65530,
	V12:  TP(uint32(65530)),
	V13:  655306553,
	V14:  TP(uint32(655306553)),
	V15:  6553065535,
	V16:  TP(uint64(6553065535)),
	V17:  true,
	V18:  TP(true),
	V19:  "hello world",
	V20:  TP("hello world"),
	V21:  []byte("hello world123456"),
	V22:  []byte("hello world123456"),
	V23:  3.1415926535895781,
	V24:  TP(float32(-3.1415926535895781)),
	V25:  3.1415926535895781,
	V26:  TP(float64(-3.1415926535895781)),
	V27:  uint64(time.Now().UnixMicro()),
	V28:  TP(uint64(time.Now().UnixMicro())),
	V29:  "3.1415926535",
	V30:  "-3.14159265359",
	V31:  &ProtoSubTest{V1: 12, V2: int32(12)},
	V32:  &ProtoSubTest{V1: 45, V2: int32(45)},
	V33:  []int32{1, 2, 3},
	V34:  []int32{1, 2, 3},
	V35:  []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V36:  []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V37:  []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V38:  []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V39:  []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V40:  []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V41:  []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V42:  []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V43:  []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V44:  []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V45:  []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V46:  []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V47:  []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V48:  []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V49:  []bool{true, false},
	V50:  []bool{true, false},
	V51:  []string{"hello world", "hello world123456", "hello world123456"},
	V52:  []string{"hello world", "hello world123456", "hello world123456"},
	V53:  [][]byte{{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
	V54:  [][]byte{{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
	V55:  []float32{1.58817, 2.58817, 3.58817, 4.58817, -5.58817, 6.58817, -7.58817, 8.58817, 9.58817, 10.58817},
	V56:  []float32{1.58817, 2.58817, 3.58817, 4.58817, -5.58817, 6.58817, -7.58817, 8.58817, 9.58817, 10.58817},
	V57:  []float64{1.58817, 2.58817, 3.58817, 4.58817, 5.58817, 6.58817, 7.58817, 8.58817, 9.58817, 10.58817},
	V58:  []float64{1.58817, 2.58817, 3.58817, 4.58817, 5.58817, 6.58817, 7.58817, 8.58817, 9.58817, 10.58817},
	V59:  []uint64{uint64(time.Now().UnixMicro()), uint64(time.Now().UnixMicro()), uint64(time.Now().UnixMicro())},
	V60:  []uint64{uint64(time.Now().UnixMicro()), uint64(time.Now().UnixMicro()), uint64(time.Now().UnixMicro())},
	V61:  []string{"3.1415926535", "3.14159265359", "3.14159265359"},
	V62:  []string{"3.1415926535", "3.14159265359", "3.14159265359"},
	V63:  []*ProtoSubTest{{V1: 12, V2: int32(12)}, {V1: 45, V2: int32(45)}, {V1: 78, V2: int32(78)}},
	V64:  []*ProtoSubTest{{V1: 12, V2: int32(12)}, {V1: 45, V2: int32(45)}, {V1: 78, V2: int32(78)}},
	V65:  map[int64]int32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V66:  map[int64]int32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V67:  map[int64]int32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V68:  map[int64]int32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V69:  map[int64]int32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V70:  map[int64]int32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V71:  map[int64]int64{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V72:  map[int64]int64{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V73:  map[int64]uint32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V74:  map[int64]uint32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V75:  map[int64]uint32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V76:  map[int64]uint32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V77:  map[int64]uint32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V78:  map[int64]uint32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V79:  map[int64]uint64{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V80:  map[int64]uint64{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V81:  map[int64]bool{1: true, 2: false, 3: true, 4: false, 5: true, 6: false, 7: true, 8: false, 9: true, 10: false},
	V82:  map[int64]bool{1: true, 2: false, 3: true, 4: false, 5: true, 6: false, 7: true, 8: false, 9: true, 10: false},
	V83:  map[int64]string{1: "1", 2: "2", 3: "3", 4: "4", 5: "5", 6: "6", 7: "7", 8: "8", 9: "9", 10: "10"},
	V84:  map[int64]string{1: "1", 2: "2", 3: "3", 4: "4", 5: "5", 6: "6", 7: "7", 8: "8", 9: "9", 10: "10"},
	V85:  map[int64][]byte{1: {1, 2}, 2: {3, 4}, 3: {5, 6}, 4: {7, 8}, 5: {9, 10}},
	V86:  map[int64][]byte{1: {1, 2}, 2: {3, 4}, 3: {5, 6}, 4: {7, 8}, 5: {9, 10}},
	V87:  map[int64]float32{1: 1.1, 2: 2.2, 3: 3.3, 4: 4.4, 5: 5.5},
	V88:  map[int64]float32{1: 1.1, 2: 2.2, 3: 3.3, 4: 4.4, 5: 5.5},
	V89:  map[int64]float64{1: 1.1, 2: 2.2, 3: 3.3, 4: 4.4, 5: 5.5},
	V90:  map[int64]float64{1: 1.1, 2: 2.2, 3: 3.3, 4: 4.4, 5: 5.5},
	V91:  map[int64]uint64{1: uint64(time.Now().UnixMicro()), 2: uint64(time.Now().UnixMicro()), 3: uint64(time.Now().UnixMicro())},
	V92:  map[int64]uint64{1: uint64(time.Now().UnixMicro()), 2: uint64(time.Now().UnixMicro()), 3: uint64(time.Now().UnixMicro())},
	V93:  map[int64]string{1: "3.1415926535", 2: "3.14159265359", 3: "3.14159265359"},
	V94:  map[int64]string{1: "3.1415926535", 2: "3.14159265359", 3: "3.14159265359"},
	V95:  map[int64]*ProtoSubTest{1: {V1: 22, V2: int32(11)}, 2: {V1: 33, V2: int32(22)}, 3: {V1: 44, V2: int32(33)}},
	V96:  map[int64]*ProtoSubTest{1: {V1: 22, V2: int32(11)}, 2: {V1: 33, V2: int32(22)}, 3: {V1: 44, V2: int32(33)}},
	V97:  map[string]int32{"a": 1, "b": 2, "c": 3},
	V98:  map[string]int32{"a": 1, "b": 2, "c": 3},
	V99:  map[string]int32{"a": 1, "b": 2, "c": 3},
	V100: map[string]int32{"a": 1, "b": 2, "c": 3},
	V101: map[string]int32{"a": 1, "b": 2, "c": 3},
	V102: map[string]int32{"a": 1, "b": 2, "c": 3},
	V103: map[string]int64{"a": 1, "b": 2, "c": 3},
	V104: map[string]int64{"a": 1, "b": 2, "c": 3},
	V105: map[string]uint32{"a": 1, "b": 2, "c": 3},
	V106: map[string]uint32{"a": 1, "b": 2, "c": 3},
	V107: map[string]uint32{"a": 1, "b": 2, "c": 3},
	V108: map[string]uint32{"a": 1, "b": 2, "c": 3},
	V109: map[string]uint32{"a": 1, "b": 2, "c": 3},
	V110: map[string]uint32{"a": 1, "b": 2, "c": 3},
	V111: map[string]uint64{"a": 1, "b": 2, "c": 3},
	V112: map[string]uint64{"a": 1, "b": 2, "c": 3},
	V113: map[string]bool{"a": true, "b": false, "c": true},
	V114: map[string]bool{"a": true, "b": false, "c": true},
	V115: map[string]string{"a": "1", "b": "2", "c": "3"},
	V116: map[string]string{"a": "1", "b": "2", "c": "3"},
	V117: map[string][]byte{"a": {1, 2}, "b": {3, 4}, "c": {5, 6}},
	V118: map[string][]byte{"a": {1, 2}, "b": {3, 4}, "c": {5, 6}},
	V119: map[string]float32{"a": 1.1, "b": 2.2, "c": 3.3},
	V120: map[string]float32{"a": 1.1, "b": 2.2, "c": 3.3},
	V121: map[string]float64{"a": 1.1, "b": 2.2, "c": 3.3},
	V122: map[string]float64{"a": 1.1, "b": 2.2, "c": 3.3},
	V123: map[string]uint64{"a": uint64(time.Now().UnixMicro()), "b": uint64(time.Now().UnixMicro()), "c": uint64(time.Now().UnixMicro())},
	V124: map[string]uint64{"a": uint64(time.Now().UnixMicro()), "b": uint64(time.Now().UnixMicro()), "c": uint64(time.Now().UnixMicro())},
	V125: map[string]string{"a": "3.1415926535", "b": "3.14159265359", "c": "3.14159265359"},
	V126: map[string]string{"a": "3.1415926535", "b": "3.14159265359", "c": "3.14159265359"},
	V127: map[string]*ProtoSubTest{"a": {V1: 22, V2: int32(11)}, "b": {V1: 33, V2: int32(22)}, "c": {V1: 44, V2: int32(33)}},
	V128: map[string]*ProtoSubTest{"a": {V1: 22, V2: int32(11)}, "b": {V1: 33, V2: int32(22)}, "c": {V1: 44, V2: int32(33)}},
}

func TestName(t *testing.T) {
	var err error
	var pBuf []byte
	t.Run("EncoderProto", func(t *testing.T) {
		pBuf, err = proto.Marshal(&pSrc)
		if err != nil {
			t.Error(err.Error())
			return
		}
		t.Log("len:", len(pBuf))
		t.Log("data:", pBuf)
	})
	t.Run("DecoderProto", func(t *testing.T) {
		des := ProtoTest{}
		err = proto.Unmarshal(pBuf, &des)
		if err != nil {
			t.Error(err.Error() + "\n" + string(pBuf))
			return
		}
	})

	jText := ""
	t.Run("EncoderJson", func(t *testing.T) {
		buf, err := json.Marshal(&src)
		if err != nil {
			t.Error(err.Error() + "\n" + string(buf))
			return
		}
		t.Log("len:", len(buf))
		t.Log("EncoderJson:", string(buf))
		jText = string(buf)
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

	hText := ""
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
		hText = string(buf)
	})
	if hText != jText {
		t.Error(jText + " != " + hText)
	}
}

func BenchmarkName(b *testing.B) {
	var err error
	var pBuf []byte
	b.Run("EncoderProto", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			pBuf, err = proto.Marshal(&pSrc)
			if err != nil {
				b.Error(err.Error() + "\n" + string(pBuf))
				return
			}
		}
	})
	b.Run("DecoderProto", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			des := ProtoTest{}
			err := proto.Unmarshal(pBuf, &des)
			if err != nil {
				b.Error(err.Error() + "\n" + string(pBuf))
				return
			}
		}
	})

	var jBuf *bytes.Buffer
	b.Run("EncoderJson", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			jBuf = bytes.NewBuffer(nil)
			err := json.NewEncoder(jBuf).Encode(&src)
			if err != nil {
				b.Error(err.Error() + "\n" + jBuf.String())
				return
			}
		}
	})
	b.Run("DecoderJson", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			des := HBufTest{}
			err := json.NewDecoder(bytes.NewReader(jBuf.Bytes())).Decode(&des)
			if err != nil {
				b.Error(err.Error())
				return
			}

		}
	})

	var hBuf []byte
	b.Run("EncoderHBuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			hBuf, err = hbuf.Marshal(&src, "")
			if err != nil {
				b.Error(err.Error() + "\n" + string(hBuf))
				return
			}
		}
	})
	b.Run("DecoderHBuf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			des := HBufTest{}
			err = hbuf.Unmarshal(hBuf, &des, "")
			if err != nil {
				b.Error(err.Error())
				return
			}
		}
	})
}
