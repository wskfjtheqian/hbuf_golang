package hbuf_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"google.golang.org/protobuf/proto"
)

var src = HBufTest{
	V1:   12,
	V2:   new(int8(-12)),
	V3:   1234,
	V4:   new(int16(-1234)),
	V5:   123456,
	V6:   new(int32(-123456)),
	V7:   123456789,
	V8:   new(int64(-123456789)),
	V9:   200,
	V10:  new(uint8(200)),
	V11:  65530,
	V12:  new(uint16(65530)),
	V13:  655306553,
	V14:  new(uint32(655306553)),
	V15:  6553065535,
	V16:  new(uint64(6553065535)),
	V17:  true,
	V18:  new(true),
	V19:  "hello world",
	V20:  new("hello world"),
	V21:  []byte("hello world123456"),
	V22:  new([]byte("hello world123456")),
	V23:  3.1415926535895781,
	V24:  new(float32(-3.1415926535895781)),
	V25:  3.1415926535895781,
	V26:  new(float64(-3.1415926535895781)),
	V27:  time.Time(time.Now()),
	V28:  new(time.Time(time.Now())),
	V29:  decimal.NewFromFloat(3.1415926535),
	V30:  new(decimal.NewFromFloat(-3.14159265359)),
	V31:  HBufSubTest{A1: 12, A2: new(int64(12))},
	V32:  &HBufSubTest{A1: 45, A2: new(int64(45))},
	V33:  []int8{1, 2, 3},
	V34:  []*int8{new(int8(1)), new(int8(2)), new(int8(3))},
	V35:  []int16{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V36:  []*int16{new(int16(1)), new(int16(2)), new(int16(3)), new(int16(4)), new(int16(5)), new(int16(6)), new(int16(7)), new(int16(8)), new(int16(9)), new(int16(10))},
	V37:  []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V38:  []*int32{new(int32(1)), new(int32(2)), new(int32(3)), new(int32(4)), new(int32(5)), new(int32(6)), new(int32(7)), new(int32(8)), new(int32(9)), new(int32(10))},
	V39:  []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V40:  []*int64{new(int64(1)), new(int64(2)), new(int64(3)), new(int64(4)), new(int64(5)), new(int64(6)), new(int64(7)), new(int64(8)), new(int64(9)), new(int64(10))},
	V41:  []uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V42:  []*uint8{new(uint8(1)), new(uint8(2)), new(uint8(3))},
	V43:  []uint16{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V44:  []*uint16{new(uint16(1)), new(uint16(2)), new(uint16(3)), new(uint16(4)), new(uint16(5)), new(uint16(6)), new(uint16(7)), new(uint16(8)), new(uint16(9)), new(uint16(10))},
	V45:  []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V46:  []*uint32{new(uint32(1)), new(uint32(2))},
	V47:  []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	V48:  []*uint64{new(uint64(1)), new(uint64(2))},
	V49:  []bool{true, false},
	V50:  []*bool{new(true), new(false)},
	V51:  []string{"hello world", "hello world123456", "hello world123456"},
	V52:  []*string{new("hello world"), new("hello world123456"), new("hello world123456")},
	V53:  [][]byte{{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
	V54:  []*[]byte{{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
	V55:  []float32{1.58817, 2.58817, 3.58817, 4.58817, -5.58817, 6.58817, -7.58817, 8.58817, 9.58817, 10.58817},
	V56:  []*float32{new(float32(1.58817)), new(float32(2.58817)), new(float32(3.58817)), new(float32(4.58817)), new(float32(5.58817)), new(float32(6.58817)), new(float32(7.58817)), new(float32(8.58817)), new(float32(9.58817)), new(float32(10.58817))},
	V57:  []float64{1.58817, 2.58817, 3.58817, 4.58817, 5.58817, 6.58817, 7.58817, 8.58817, 9.58817, 10.58817},
	V58:  []*float64{new(float64(1.58817)), new(float64(2.58817)), new(float64(3.58817)), new(float64(4.58817)), new(float64(5.58817)), new(float64(6.58817)), new(float64(7.58817)), new(float64(8.58817)), new(float64(9.58817)), new(float64(10.58817))},
	V59:  []time.Time{time.Time(time.Now()), time.Time(time.Now()), time.Time(time.Now())},
	V60:  []*time.Time{new(time.Time(time.Now())), new(time.Time(time.Now())), new(time.Time(time.Now()))},
	V61:  []decimal.Decimal{decimal.NewFromFloat(3.1415926535), decimal.NewFromFloat(3.14159265359), decimal.NewFromFloat(3.14159265359)},
	V62:  []*decimal.Decimal{new(decimal.NewFromFloat(3.1415926535)), new(decimal.NewFromFloat(3.14159265359)), new(decimal.NewFromFloat(3.14159265359))},
	V63:  []HBufSubTest{{A1: 12, A2: new(int64(12))}, {A1: 45, A2: new(int64(45))}, {A1: 78, A2: new(int64(78))}},
	V64:  []*HBufSubTest{{A1: 12, A2: new(int64(12))}, {A1: 45, A2: new(int64(45))}, {A1: 78, A2: new(int64(78))}},
	V65:  map[int64]int8{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V66:  map[int64]*int8{1: new(int8(1)), 2: new(int8(2)), 3: new(int8(3)), 4: new(int8(4)), 5: new(int8(5)), 6: new(int8(6)), 7: new(int8(7)), 8: new(int8(8)), 9: new(int8(9)), 10: new(int8(10))},
	V67:  map[int64]int16{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V68:  map[int64]*int16{1: new(int16(1)), 2: new(int16(2)), 3: new(int16(3)), 4: new(int16(4)), 5: new(int16(5)), 6: new(int16(6)), 7: new(int16(7)), 8: new(int16(8)), 9: new(int16(9)), 10: new(int16(10))},
	V69:  map[int64]int32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V70:  map[int64]*int32{1: new(int32(1)), 2: new(int32(2)), 3: new(int32(3)), 4: new(int32(4)), 5: new(int32(5)), 6: new(int32(6)), 7: new(int32(7)), 8: new(int32(8)), 9: new(int32(9)), 10: new(int32(10))},
	V71:  map[int64]int64{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V72:  map[int64]*int64{1: new(int64(1)), 2: new(int64(2)), 3: new(int64(3)), 4: new(int64(4)), 5: new(int64(5)), 6: new(int64(6)), 7: new(int64(7)), 8: new(int64(8)), 9: new(int64(9)), 10: new(int64(10))},
	V73:  map[int64]uint8{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V74:  map[int64]*uint8{1: new(uint8(1)), 2: new(uint8(2)), 3: new(uint8(3)), 4: new(uint8(4)), 5: new(uint8(5)), 6: new(uint8(6)), 7: new(uint8(7)), 8: new(uint8(8)), 9: new(uint8(9)), 10: new(uint8(10))},
	V75:  map[int64]uint16{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V76:  map[int64]*uint16{1: new(uint16(1)), 2: new(uint16(2)), 3: new(uint16(3)), 4: new(uint16(4)), 5: new(uint16(5)), 6: new(uint16(6)), 7: new(uint16(7)), 8: new(uint16(8)), 9: new(uint16(9)), 10: new(uint16(10))},
	V77:  map[int64]uint32{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V78:  map[int64]*uint32{1: new(uint32(1)), 2: new(uint32(2)), 3: new(uint32(3)), 4: new(uint32(4)), 5: new(uint32(5)), 6: new(uint32(6)), 7: new(uint32(7)), 8: new(uint32(8)), 9: new(uint32(9)), 10: new(uint32(10))},
	V79:  map[int64]uint64{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10},
	V80:  map[int64]*uint64{1: new(uint64(1)), 2: new(uint64(2)), 3: new(uint64(3)), 4: new(uint64(4)), 5: new(uint64(5)), 6: new(uint64(6)), 7: new(uint64(7)), 8: new(uint64(8)), 9: new(uint64(9)), 10: new(uint64(10))},
	V81:  map[int64]bool{1: true, 2: false, 3: true, 4: false, 5: true, 6: false, 7: true, 8: false, 9: true, 10: false},
	V82:  map[int64]*bool{1: new(true), 2: new(false), 3: new(true), 4: new(false), 5: new(true), 6: new(false), 7: new(true), 8: new(false), 9: new(true), 10: new(false)},
	V83:  map[int64]string{1: "1", 2: "2", 3: "3", 4: "4", 5: "5", 6: "6", 7: "7", 8: "8", 9: "9", 10: "10"},
	V84:  map[int64]*string{1: new("1"), 2: new("2"), 3: new("3"), 4: new("4"), 5: new("5"), 6: new("6"), 7: new("7"), 8: new("8"), 9: new("9"), 10: new("10")},
	V85:  map[int64][]byte{1: {1, 2}, 2: {3, 4}, 3: {5, 6}, 4: {7, 8}, 5: {9, 10}},
	V86:  map[int64]*[]byte{1: new([]byte{1, 2}), 2: new([]byte{3, 4}), 3: new([]byte{5, 6}), 4: new([]byte{7, 8}), 5: new([]byte{9, 10})},
	V87:  map[int64]float32{1: 1.1, 2: 2.2, 3: 3.3, 4: 4.4, 5: 5.5},
	V88:  map[int64]*float32{1: new(float32(1.1)), 2: new(float32(2.2)), 3: new(float32(3.3)), 4: new(float32(4.4)), 5: new(float32(5.5))},
	V89:  map[int64]float64{1: 1.1, 2: 2.2, 3: 3.3, 4: 4.4, 5: 5.5},
	V90:  map[int64]*float64{1: new(float64(1.1)), 2: new(float64(2.2)), 3: new(float64(3.3)), 4: new(float64(4.4)), 5: new(float64(5.5))},
	V91:  map[int64]time.Time{1: time.Now(), 2: time.Now(), 3: time.Now()},
	V92:  map[int64]*time.Time{1: new(time.Now()), 2: new(time.Now()), 3: new(time.Now())},
	V93:  map[int64]decimal.Decimal{1: decimal.NewFromFloat(3.1415926535), 2: decimal.NewFromFloat(3.14159265359), 3: decimal.NewFromFloat(3.14159265359)},
	V94:  map[int64]*decimal.Decimal{1: new(decimal.NewFromFloat(3.1415926535)), 2: new(decimal.NewFromFloat(3.14159265359)), 3: new(decimal.NewFromFloat(3.14159265359))},
	V95:  map[int64]HBufSubTest{1: {A1: 22, A2: new(int64(11))}, 2: {A1: 33, A2: new(int64(22))}, 3: {A1: 44, A2: new(int64(33))}},
	V96:  map[int64]*HBufSubTest{1: new(HBufSubTest{A1: 22, A2: new(int64(11))}), 2: new(HBufSubTest{A1: 33, A2: new(int64(22))}), 3: new(HBufSubTest{A1: 44, A2: new(int64(33))})},
	V97:  map[string]int8{"a": 1, "b": 2, "c": 3},
	V98:  map[string]*int8{"a": new(int8(1)), "b": new(int8(2)), "c": new(int8(3))},
	V99:  map[string]int16{"a": 1, "b": 2, "c": 3},
	V100: map[string]*int16{"a": new(int16(1)), "b": new(int16(2)), "c": new(int16(3))},
	V101: map[string]int32{"a": 1, "b": 2, "c": 3},
	V102: map[string]*int32{"a": new(int32(1)), "b": new(int32(2)), "c": new(int32(3))},
	V103: map[string]int64{"a": 1, "b": 2, "c": 3},
	V104: map[string]*int64{"a": new(int64(1)), "b": new(int64(2)), "c": new(int64(3))},
	V105: map[string]uint8{"a": 1, "b": 2, "c": 3},
	V106: map[string]*uint8{"a": new(uint8(1)), "b": new(uint8(2)), "c": new(uint8(3))},
	V107: map[string]uint16{"a": 1, "b": 2, "c": 3},
	V108: map[string]*uint16{"a": new(uint16(1)), "b": new(uint16(2)), "c": new(uint16(3))},
	V109: map[string]uint32{"a": 1, "b": 2, "c": 3},
	V110: map[string]*uint32{"a": new(uint32(1)), "b": new(uint32(2)), "c": new(uint32(3))},
	V111: map[string]uint64{"a": 1, "b": 2, "c": 3},
	V112: map[string]*uint64{"a": new(uint64(1)), "b": new(uint64(2)), "c": new(uint64(3))},
	V113: map[string]bool{"a": true, "b": false, "c": true},
	V114: map[string]*bool{"a": new(true), "b": new(false), "c": new(true)},
	V115: map[string]string{"a": "1", "b": "2", "c": "3"},
	V116: map[string]*string{"a": new("1"), "b": new("2"), "c": new("3")},
	V117: map[string][]byte{"a": {1, 2}, "b": {3, 4}, "c": {5, 6}},
	V118: map[string]*[]byte{"a": new([]byte{1, 2}), "b": new([]byte{3, 4}), "c": new([]byte{5, 6})},
	V119: map[string]float32{"a": 1.1, "b": 2.2, "c": 3.3},
	V120: map[string]*float32{"a": new(float32(1.1)), "b": new(float32(2.2)), "c": new(float32(3.3))},
	V121: map[string]float64{"a": 1.1, "b": 2.2, "c": 3.3},
	V122: map[string]*float64{"a": new(float64(1.1)), "b": new(float64(2.2)), "c": new(float64(3.3))},
	V123: map[string]time.Time{"a": time.Time(time.Now()), "b": time.Time(time.Now()), "c": time.Time(time.Now())},
	V124: map[string]*time.Time{"a": new(time.Time(time.Now())), "b": new(time.Time(time.Now())), "c": new(time.Time(time.Now()))},
	V125: map[string]decimal.Decimal{"a": decimal.NewFromFloat(3.1415926535), "b": decimal.NewFromFloat(3.14159265359), "c": decimal.NewFromFloat(3.14159265359)},
	V126: map[string]*decimal.Decimal{"a": new(decimal.NewFromFloat(3.1415926535)), "b": new(decimal.NewFromFloat(3.14159265359)), "c": new(decimal.NewFromFloat(3.14159265359))},
	V127: map[string]HBufSubTest{"a": {A1: 22, A2: new(int64(11))}, "b": {A1: 33, A2: new(int64(22))}, "c": {A1: 44, A2: new(int64(33))}},
	V128: map[string]*HBufSubTest{"a": new(HBufSubTest{A1: 22, A2: new(int64(11))}), "b": new(HBufSubTest{A1: 33, A2: new(int64(22))}), "c": new(HBufSubTest{A1: 44, A2: new(int64(33))})},
}

var pSrc = ProtoTest{
	V1:   12,
	V2:   new(int32(-12)),
	V3:   1234,
	V4:   new(int32(-1234)),
	V5:   123456,
	V6:   new(int32(-123456)),
	V7:   123456789,
	V8:   new(int64(-123456789)),
	V9:   200,
	V10:  new(uint32(200)),
	V11:  65530,
	V12:  new(uint32(65530)),
	V13:  655306553,
	V14:  new(uint32(655306553)),
	V15:  6553065535,
	V16:  new(uint64(6553065535)),
	V17:  true,
	V18:  new(true),
	V19:  "hello world",
	V20:  new("hello world"),
	V21:  []byte("hello world123456"),
	V22:  []byte("hello world123456"),
	V23:  3.1415926535895781,
	V24:  new(float32(-3.1415926535895781)),
	V25:  3.1415926535895781,
	V26:  new(float64(-3.1415926535895781)),
	V27:  uint64(time.Now().UnixMicro()),
	V28:  new(uint64(time.Now().UnixMicro())),
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
	t.Run("DecoderHBuf", func(t *testing.T) {
		hBuf, err = os.ReadFile(filepath.Join(os.TempDir(), "test.bin"))
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
		des := HBufTest{}
		err = hbuf.Unmarshal(hBuf, &des)
		if err != nil {
			t.Error(err.Error() + "\n")
			return
		}
		buf, _ := json.Marshal(&des)
		hText = string(buf)
	})
	if hText != jText {
		t.Error("jText +  !=  + hText")
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

	sJson, err := json.Marshal(&eSrc)
	if err != nil {
		t.Error(err.Error() + "\n")
		return
	}
	desJson, err := json.Marshal(&des)
	if err != nil {
		t.Error(err.Error() + "\n")
		return
	}

	t.Log(string(sJson))
	t.Log(string(desJson))
	if string(sJson) != string(desJson) {
		t.Error("not equal")
		return
	}
}
