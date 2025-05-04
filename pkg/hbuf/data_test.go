package hbuf_test

import (
	"bytes"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"google.golang.org/genproto/googleapis/type/decimal"
	"testing"
)

var testDataHBufStruct = map[uint16]*hbuf.Description{
	11: hbuf.NewFieldDescription[int8](hbuf.WriterInt8, hbuf.ReaderInt8, hbuf.LengthInt8),
	12: hbuf.NewFieldDescription[int16](hbuf.WriterInt16, hbuf.ReaderInt16, hbuf.LengthInt16),
	13: hbuf.NewFieldDescription[int16](hbuf.WriterInt32, hbuf.ReaderInt32, hbuf.LengthInt32),
	14: hbuf.NewFieldDescription[hbuf.Int64](hbuf.WriterInt64, hbuf.ReaderInt64, hbuf.LengthInt64),
	15: hbuf.NewFieldDescription[uint8](hbuf.WriterUint8, hbuf.ReaderUint8, hbuf.LengthUint8),
	16: hbuf.NewFieldDescription[uint16](hbuf.WriterUint16, hbuf.ReaderUint16, hbuf.LengthUint16),
	17: hbuf.NewFieldDescription[uint32](hbuf.WriterUint32, hbuf.ReaderUint32, hbuf.LengthUint32),
	18: hbuf.NewFieldDescription[hbuf.Uint64](hbuf.WriterUint64, hbuf.ReaderUint64, hbuf.LengthUint64),
	19: hbuf.NewFieldDescription[[]byte](hbuf.WriterBytes, hbuf.ReaderBytes, hbuf.LengthBytes),
	20: hbuf.NewFieldDescription[string](hbuf.WriterString, hbuf.ReaderString, hbuf.LengthString),
	21: hbuf.NewFieldDescription[hbuf.Time](hbuf.WriterTime, hbuf.ReaderTime, hbuf.LengthTime),
	22: hbuf.NewFieldDescription[decimal.Decimal](hbuf.WriterDecimal, hbuf.ReaderDecimal, hbuf.LengthDecimal),
	23: hbuf.NewFieldDescription[bool](hbuf.WriterBool, hbuf.ReaderBool, hbuf.LengthBool),

	31: hbuf.NewListDescription[int8](hbuf.WriterInt8, hbuf.ReaderInt8, hbuf.LengthInt8),
	32: hbuf.NewListDescription[int16](hbuf.WriterInt16, hbuf.ReaderInt16, hbuf.LengthInt16),
	33: hbuf.NewListDescription[int16](hbuf.WriterInt32, hbuf.ReaderInt32, hbuf.LengthInt32),
	34: hbuf.NewListDescription[hbuf.Int64](hbuf.WriterInt64, hbuf.ReaderInt64, hbuf.LengthInt64),
	35: hbuf.NewListDescription[uint8](hbuf.WriterUint8, hbuf.ReaderUint8, hbuf.LengthUint8),
	36: hbuf.NewListDescription[uint16](hbuf.WriterUint16, hbuf.ReaderUint16, hbuf.LengthUint16),
	37: hbuf.NewListDescription[uint32](hbuf.WriterUint32, hbuf.ReaderUint32, hbuf.LengthUint32),
	38: hbuf.NewListDescription[hbuf.Uint64](hbuf.WriterUint64, hbuf.ReaderUint64, hbuf.LengthUint64),
	39: hbuf.NewListDescription[[]byte](hbuf.WriterBytes, hbuf.ReaderBytes, hbuf.LengthBytes),
	40: hbuf.NewListDescription[string](hbuf.WriterString, hbuf.ReaderString, hbuf.LengthString),
	41: hbuf.NewListDescription[hbuf.Time](hbuf.WriterTime, hbuf.ReaderTime, hbuf.LengthTime),
	42: hbuf.NewListDescription[decimal.Decimal](hbuf.WriterDecimal, hbuf.ReaderDecimal, hbuf.LengthDecimal),
	43: hbuf.NewListDescription[bool](hbuf.WriterBool, hbuf.ReaderBool, hbuf.LengthBool),
}

type TestData struct {
	V11 int8            `json:"v1,omitempty" hbuf:"11"`
	V12 int16           `json:"v1,omitempty" hbuf:"12"`
	V13 int32           `json:"v1,omitempty" hbuf:"13"`
	V14 hbuf.Int64      `json:"v1,omitempty" hbuf:"14"`
	V15 uint8           `json:"v1,omitempty" hbuf:"15"`
	V16 uint16          `json:"v1,omitempty" hbuf:"16"`
	V17 uint32          `json:"v1,omitempty" hbuf:"17"`
	V18 uint64          `json:"v1,omitempty" hbuf:"18"`
	V19 []byte          `json:"v1,omitempty" hbuf:"19"`
	V20 string          `json:"v1,omitempty" hbuf:"20"`
	V21 hbuf.Time       `json:"v1,omitempty" hbuf:"21"`
	V22 decimal.Decimal `json:"v1,omitempty" hbuf:"22"`
	V23 bool            `json:"v1,omitempty" hbuf:"23"`

	V31 []int8            `json:"v1,omitempty" hbuf:"31"`
	V32 []int16           `json:"v1,omitempty" hbuf:"32"`
	V33 []int32           `json:"v1,omitempty" hbuf:"33"`
	V34 []hbuf.Int64      `json:"v1,omitempty" hbuf:"34"`
	V35 []uint8           `json:"v1,omitempty" hbuf:"35"`
	V36 []uint16          `json:"v1,omitempty" hbuf:"36"`
	V37 []uint32          `json:"v1,omitempty" hbuf:"37"`
	V38 []uint64          `json:"v1,omitempty" hbuf:"38"`
	V39 [][]byte          `json:"v1,omitempty" hbuf:"39"`
	V40 []string          `json:"v1,omitempty" hbuf:"40"`
	V41 []hbuf.Time       `json:"v1,omitempty" hbuf:"41"`
	V42 []decimal.Decimal `json:"v1,omitempty" hbuf:"42"`
	V43 []bool            `json:"v1,omitempty" hbuf:"43"`
}

func (d *TestData) Description() map[uint16]*hbuf.Description {
	return testDataHBufStruct
}

func Test_EncodeData(t *testing.T) {
	d := TestData{
		V1: 02,
	}

	err := hbuf.NewEncoder(bytes.NewBuffer(nil)).Encode(&d)
	if err != nil {
		t.Error(err)
		return
	}

	return
}
