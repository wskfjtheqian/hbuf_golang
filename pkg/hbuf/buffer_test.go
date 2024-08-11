package hbuf

import (
	"bytes"
	"strconv"
	"testing"
)

func TestDecoderField(t *testing.T) {
	for typ := Type(0); typ < TExtend+1; typ++ {
		for valueLen := uint8(1); valueLen < 4; valueLen++ {
			for id := uint16(0); id < 0xffff; id++ {
				data := bytes.NewBuffer(nil)
				err := WriterField(data, typ, id, valueLen)
				if err != nil {
					t.Fatalf("failed to encode field %v: %v", typ, err)
				}

				dTyp, dId, dLen, err := ReaderField(data)
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
	for i := int64(0); i < 0xFFFFF; i++ {
		b := EncoderInt64(i)
		value := DecoderInt64(b)
		if value != i {
			t.Fatal("Decoder int64 error type = " + strconv.Itoa(int(value)))
		}
	}

}
func TestDecoderUnt64(t *testing.T) {
	for i := uint64(0); i < 0xFFFFF; i++ {
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
