package hbuf_test

import (
	"bytes"
	"encoding/json"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"io"
	"math/rand"
	"testing"
)

type subStruct struct {
	ValueInt int `json:"ValueInt,omitempty"`
}

func (t *subStruct) Encoder(w io.Writer) error {
	err := hbuf.WriterInt64(w, 1, int64(t.ValueInt))
	if err != nil {
		return err
	}
	return nil
}

func (s *subStruct) Decoder(r io.Reader) error {
	//TODO implement me
	panic("implement me")
}

func (s *subStruct) Size() int {
	//TODO implement me
	panic("implement me")
}

type testStruct struct {
	String       `json:"Name,omitempty"`
	ValueInt     int       `json:"ValueInt,omitempty"`
	ValueInt8    int8      `json:"ValueInt8,omitempty"`
	ValueInt16   int16     `json:"ValueInt16,omitempty"`
	ValueInt32   int32     `json:"ValueInt32,omitempty"`
	ValueInt64   int64     `json:"ValueInt64,omitempty"`
	ValueUint    uint      `json:"ValueUint,omitempty"`
	ValueUint8   uint8     `json:"ValueUint8,omitempty"`
	ValueUint16  uint16    `json:"ValueUint16,omitempty"`
	ValueUint32  uint32    `json:"ValueUint32,omitempty"`
	ValueUint64  uint64    `json:"ValueUint64,omitempty"`
	ValueFloat32 float32   `json:"ValueFloat32,omitempty"`
	ValueFloat64 float64   `json:"ValueFloat64,omitempty"`
	ValueString  string    `json:"ValueString,omitempty"`
	ValueBytes   []byte    `json:"ValueBytes,omitempty"`
	ValueBool    bool      `json:"ValueBool,omitempty"`
	ValueData    subStruct `json:"ValueData,omitempty"`
}

func (t *testStruct) Encoder(w io.Writer) error {
	err := hbuf.WriterInt64(w, 1, int64(t.ValueInt))
	if err != nil {
		return err
	}
	err = hbuf.WriterInt64(w, 2, int64(t.ValueInt8))
	if err != nil {
		return err
	}
	err = hbuf.WriterInt64(w, 3, int64(t.ValueInt16))
	if err != nil {
		return err
	}
	err = hbuf.WriterInt64(w, 4, int64(t.ValueInt32))
	if err != nil {
		return err
	}
	err = hbuf.WriterInt64(w, 5, int64(t.ValueInt64))
	if err != nil {
		return err
	}
	err = hbuf.WriterUint64(w, 6, uint64(t.ValueUint))
	if err != nil {
		return err
	}
	err = hbuf.WriterUint64(w, 7, uint64(t.ValueUint8))
	if err != nil {
		return err
	}
	err = hbuf.WriterUint64(w, 8, uint64(t.ValueUint16))
	if err != nil {
		return err
	}
	err = hbuf.WriterUint64(w, 9, uint64(t.ValueUint32))
	if err != nil {
		return err
	}
	err = hbuf.WriterUint64(w, 10, uint64(t.ValueUint64))
	if err != nil {
		return err
	}
	err = hbuf.WriterFloat(w, 11, t.ValueFloat32)
	if err != nil {
		return err
	}
	err = hbuf.WriterDouble(w, 12, t.ValueFloat64)
	if err != nil {
		return err
	}
	err = hbuf.WriterBytes(w, 13, []byte(t.ValueString))
	if err != nil {
		return err
	}
	err = hbuf.WriterBytes(w, 14, t.ValueBytes)
	if err != nil {
		return err
	}
	err = hbuf.WriterBool(w, 15, t.ValueBool)
	if err != nil {
		return err
	}
	err = hbuf.WriterData(w, 16, &t.ValueData)
	if err != nil {
		return err
	}
	return nil
}

func (t *testStruct) Decoder(r io.Reader) error {
	err := hbuf.Decoder(r, func(typ hbuf.Type, id uint16, value any) (err error) {
		switch id {
		case 1:
			t.ValueInt, err = hbuf.ReaderNumber[int](value)
		case 2:
			t.ValueInt8, err = hbuf.ReaderNumber[int8](value)
		case 3:
			t.ValueInt16, err = hbuf.ReaderNumber[int16](value)
		case 4:
			t.ValueInt32, err = hbuf.ReaderNumber[int32](value)
		case 5:
			t.ValueInt64, err = hbuf.ReaderNumber[int64](value)
		case 6:
			t.ValueUint, err = hbuf.ReaderNumber[uint](value)
		case 7:
			t.ValueUint8, err = hbuf.ReaderNumber[uint8](value)
		case 8:
			t.ValueUint16, err = hbuf.ReaderNumber[uint16](value)
		case 9:
			t.ValueUint32, err = hbuf.ReaderNumber[uint32](value)
		case 10:
			t.ValueUint64, err = hbuf.ReaderNumber[uint64](value)
		case 11:
			t.ValueFloat32, err = hbuf.ReaderNumber[float32](value)
		case 12:
			t.ValueFloat64, err = hbuf.ReaderNumber[float64](value)

		case 13:
			t.ValueString, err = hbuf.ReaderBytes[string](value)
		case 14:
			t.ValueBytes, err = hbuf.ReaderBytes[[]byte](value)
		default:
		case 15:
			t.ValueBool, err = hbuf.ReaderBool(value)
		}
		return
	})
	if err != nil {
		return err
	}
	return nil
}

func (t *testStruct) Size() int {
	length := 0
	if t.ValueInt != 0 {
		length += 1 + int(hbuf.LengthInt64(int64(t.ValueInt))) + int(hbuf.LengthInt64(1))
	}
	if t.ValueInt8 != 0 {
		length += 1 + int(hbuf.LengthInt64(int64(t.ValueInt8))) + int(hbuf.LengthInt64(2))
	}
	if t.ValueInt16 != 0 {
		length += 1 + int(hbuf.LengthInt64(int64(t.ValueInt16))) + int(hbuf.LengthInt64(3))
	}
	if t.ValueInt32 != 0 {
		length += 1 + int(hbuf.LengthInt64(int64(t.ValueInt32))) + int(hbuf.LengthInt64(4))
	}
	if t.ValueInt64 != 0 {
		length += 1 + int(hbuf.LengthInt64(int64(t.ValueInt64))) + int(hbuf.LengthInt64(5))
	}
	if t.ValueUint != 0 {
		length += 1 + int(hbuf.LengthUint64(uint64(t.ValueUint))) + int(hbuf.LengthUint64(6))
	}
	if t.ValueUint8 != 0 {
		length += 1 + int(hbuf.LengthUint64(uint64(t.ValueUint8))) + int(hbuf.LengthUint64(7))
	}
	if t.ValueUint16 != 0 {
		length += 1 + int(hbuf.LengthUint64(uint64(t.ValueUint16))) + int(hbuf.LengthUint64(8))
	}
	if t.ValueUint32 != 0 {
		length += 1 + int(hbuf.LengthUint64(uint64(t.ValueUint32))) + int(hbuf.LengthUint64(9))
	}
	if t.ValueUint64 != 0 {
		length += 1 + int(hbuf.LengthUint64(uint64(t.ValueUint64))) + int(hbuf.LengthUint64(10))
	}
	if t.ValueFloat32 != 0 {
		length += 1 + int(hbuf.LengthFloat(t.ValueFloat32)) + int(hbuf.LengthInt64(11))
	}
	if t.ValueFloat64 != 0 {
		length += 1 + int(hbuf.LengthDouble(t.ValueFloat64)) + int(hbuf.LengthInt64(12))
	}
	if t.ValueString != "" {
		length += 1 + int(hbuf.LengthBytes([]byte(t.ValueString))) + int(hbuf.LengthInt64(13))
	}
	if len(t.ValueBytes) != 0 {
		length += 1 + int(hbuf.LengthBytes(t.ValueBytes)) + int(hbuf.LengthInt64(14))
	}
	if t.ValueBool {
		length += 1 + int(hbuf.LengthInt64(15))
	}
	return length
}

type String string

func TestEncoderDecoder(t *testing.T) {
	t1 := testStruct{
		String:       "string",
		ValueInt:     -2118888625,
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
		ValueString:  "最佳答案：uint16是无符号的16位整型数据类型。最佳答案：uint16是无符号的16位整型数据类型。最佳答案：uint16是无符号的16位整型数据类型。最佳答案：uint16是无符号的16位整型数据类型。",
		ValueBytes:   []byte("length += 1 + int(hbuf.LengthUint64(uint64(t.ValueUint64))) + int(hbuf.LengthUint64(10))"),
		ValueBool:    true,
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
	})

	t2 := testStruct{}
	t.Run("Decoder", func(t *testing.T) {
		err := t2.Decoder(bytes.NewReader(out.Bytes()))
		if err != nil {
			t.Error(err)
			return
		}

	})

	if t1.ValueInt != t2.ValueInt {
		t.Error("not equal ValueInt")
	}
	if t1.ValueInt8 != t2.ValueInt8 {
		t.Error("not equal ValueInt8")
	}
	if t1.ValueInt16 != t2.ValueInt16 {
		t.Error("not equal ValueInt16")
	}
	if t1.ValueInt32 != t2.ValueInt32 {
		t.Error("not equal ValueInt32")
	}
	if t1.ValueInt64 != t2.ValueInt64 {
		t.Error("not equal ValueInt64")
	}
	if t1.ValueUint != t2.ValueUint {
		t.Error("not equal ValueUint")
	}
	if t1.ValueUint8 != t2.ValueUint8 {
		t.Error("not equal ValueUint8")
	}
	if t1.ValueUint16 != t2.ValueUint16 {
		t.Error("not equal ValueUint16")
	}
	if t1.ValueUint32 != t2.ValueUint32 {
		t.Error("not equal ValueUint32")
	}
	if t1.ValueUint64 != t2.ValueUint64 {
		t.Error("not equal ValueUint64")
	}
	if t1.ValueFloat32 != t2.ValueFloat32 {
		t.Error("not equal ValueFloat32")
	}
	if t1.ValueFloat64 != t2.ValueFloat64 {
		t.Error("not equal ValueFloat64")
	}
	if t1.ValueString != t2.ValueString {
		t.Error("not equal ValueString")
	}
	if bytes.Compare(t1.ValueBytes, t2.ValueBytes) != 0 {
		t.Error("not equal ValueBytes")
	}
	if t1.ValueBool != t2.ValueBool {
		t.Error("not equal ValueBool")
	}
}

func BenchmarkEncoder(b *testing.B) {
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
		ValueString:  "最佳答案：uint16是无符号的16位整型数据类型。详细解释如下：1. 数据类型定义...最佳答案：uint16是无符号的16位整型数据类型。详细解释如下：1. 数据类型定义...最佳答案：uint16是无符号的16位整型数据类型。详细解释如下：1. 数据类型定义...最佳答案：uint16是无符号的16位整型数据类型。详细解释如下：1. 数据类型定义...最佳答案：uint16是无符号的16位整型数据类型。详细解释如下：1. 数据类型定义...最佳答案：uint16是无符号的16位整型数据类型。详细解释如下：1. 数据类型定义...",
		ValueBytes:   []byte("ValueFloat64: float64(rand.Float64()),ValueFloat64: float64(rand.Float64()),ValueFloat64: float64(rand.Float64()),ValueFloat64: float64(rand.Float64()),ValueFloat64: float64(rand.Float64()),"),
		ValueBool:    true,
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

	out := bytes.NewBuffer(make([]byte, 0, t1.Size()))
	err := t1.Encoder(out)
	if err != nil {
		b.Error(err)
		return
	}
	b.Run("EncoderHbuf", func(b *testing.B) {
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
	b.Run("EncoderHbufJSON", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t2 := testStruct{}
			err := json.NewDecoder(bytes.NewReader(out.Bytes())).Decode(&t2)
			if err != nil {
				b.Error(err)
				return
			}
		}
	})
}
