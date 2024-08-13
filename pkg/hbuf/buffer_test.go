package hbuf

import (
	"bytes"
	"io"
	"strconv"
	"testing"
)

func TestDecoderId(t *testing.T) {
	for i := uint16(0); i < 0xFFFF; i++ {
		data := bytes.NewBuffer(nil)
		err := WriteId(data, i)
		if err != nil {
			t.Fatal(err)
		}
		id, err := ReadId(data)
		if err != nil {
			t.Fatal(err)
		}
		if id != i {
			t.Fatal("Decoder id error type = " + strconv.Itoa(int(i)))
		}
	}

}

func TestDecoderField(t *testing.T) {
	for typ := Type(0); typ < TExtend+1; typ++ {
		for valueLen := uint8(0); valueLen < 4; valueLen++ {
			for id := uint16(0); id < 0xffff; id++ {
				data := bytes.NewBuffer(nil)
				err := WriteField(data, typ, id, valueLen)
				if err != nil {
					t.Fatalf("failed to encode field %v: %v", typ, err)
				}

				dTyp, dId, dLen, err := ReadField(data)
				if err != nil {
					t.Fatalf("error decoding field %d/%d: %v", typ, id, err)
				}
				if dTyp != typ || dId != id || dLen != valueLen {
					t.Fatal("Decoder field error type = " + strconv.Itoa(int(typ)) + "; valueLen = " + strconv.Itoa(int(valueLen)) + "; id = " + strconv.Itoa(int(id)))
				}
			}
		}
	}
}

func TestDecoderInt64(t *testing.T) {
	for i := int64(0); i < 0xFFFFFFFF; i++ {
		b := EncoderInt64(i)
		value := DecoderInt64(b)
		if value != i {
			t.Fatal("Decoder int64 error type = " + strconv.Itoa(int(value)))
		}
	}
}

func TestDecoderUnt64(t *testing.T) {
	for i := uint64(0); i < 0xFFFFFFFF; i++ {
		b := EncoderUint64(i)
		value := DecoderUint64(b)
		if value != i {
			t.Fatal("Decoder int64 error type = " + strconv.Itoa(int(value)))
		}
	}

}

func TestDecoderFloat(t *testing.T) {
	v := float32(3.1415926)
	b := EncoderFloat(v)
	value := DecoderFloat(b)
	if value != v {
		t.Fatal("Decoder int64 error type = " + strconv.Itoa(int(value)))
	}
}

func TestDecoderDouble(t *testing.T) {
	v := float64(3.1415926)
	b := EncoderDouble(v)
	value := DecoderDouble(b)
	if value != v {
		t.Fatal("Decoder int64 error type = " + strconv.Itoa(int(value)))
	}
}

//-------------------------------------

type BufferTest1 struct {
	Field1 string
	Field2 *string
}

func (b BufferTest1) Encoder(w io.Writer) (err error) {

	return nil
}

func (b BufferTest1) Decoder(r io.Reader) (err error) {
	//TODO implement me
	panic("implement me")
}

func TestBufferTest1(t *testing.T) {
	//b:=BufferTest1{
	//	Field1: "https://translate.google.com/?hl=zh-CN&tab=TT&sl=auto&tl=zh-CN&op=translate"
	//}
}
