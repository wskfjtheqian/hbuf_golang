package hbuf

import (
	"bytes"
	"io"
	"math"
	"testing"
)

type User struct {
	Name    String
	Age     Int8
	Height  float32
	Weight  float64
	Address []string
}

func (u *User) Encoder(id uint16) (uint32, EncoderCall) {
	var tempLen, length uint32
	var name, age EncoderCall

	tempLen, name = u.Name.Encoder(1)
	length += tempLen

	tempLen, age = u.Age.Encoder(2)
	length += tempLen

	tempLen = uint32(LengthInt64(int64(length)))

	return length + tempLen + 2, func(w io.Writer) error {
		err := WriterField(w, TData, id, uint8(tempLen))
		if err != nil {
			return err
		}

		b := EncoderInt64(int64(length))
		_, err = w.Write(b)
		if err != nil {
			return err
		}
		if name != nil {
			err := name(w)
			if err != nil {
				return err
			}
		}
		if age != nil {
			err := age(w)
			if err != nil {
				return err
			}
		}
		return nil
	}
}

func Test_Encoder(t *testing.T) {
	u := User{
		Name:   "adfasdfasdfasdfasdfasadfasdfasdfasdfasdfasadfasdfasdfasdfasdfasadfasdfasdfasdfasdfasadfasdfasdfasdfasdfas",
		Age:    12,
		Height: math.Pi,
		Weight: math.E,
	}
	w := bytes.NewBuffer(nil)
	_, user := u.Encoder(0)
	err := user(w)
	if err != nil {
		t.Fatal(err)
		return
	}
}
