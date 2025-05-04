package hbuf

import (
	"errors"
	"google.golang.org/genproto/googleapis/type/decimal"
	"io"
	"reflect"
)

var ErrUnsupportedType = errors.New("unsupported type")

type Encoder struct {
	w io.Writer
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

func (e *Encoder) Encode(v any) error {
	typ := reflect.TypeOf(v)
	switch typ.Kind() {
	case reflect.Int:
	case reflect.Int8:
	case reflect.Int16:
	case reflect.Int32:
	case reflect.Int64:
	case reflect.Uint:
	case reflect.Uint8:
	case reflect.Uint16:
	case reflect.Uint32:
	case reflect.Uint64:
	case reflect.Float32:
	case reflect.Float64:
	case reflect.String:
	case reflect.Bool:
	case reflect.Slice:
	case reflect.Array:
	case reflect.Map:
	case reflect.Struct:

	case reflect.Ptr:
		if v == nil {
			return nil
		}
		return e.Encode(reflect.Indirect(reflect.ValueOf(v)).Interface())
	default:
	}
	return nil
}

func EncodeInt8(w io.Writer, id uint16, v *int8) error {
	return nil
}

func LengthInt8(v *int8) uint {
	return 0
}

func EncodeInt16(w io.Writer, id uint16, v *int16) error {
	return nil
}

func LengthInt16(v *int16) uint {
	return 0
}

func EncodeInt32(w io.Writer, id uint16, v *int16) error {
	return nil
}

func LengthInt32(v *int16) uint {
	return 0
}

func EncodeInt64(w io.Writer, id uint16, v *Int64) error {
	return nil
}

func LengthInt64(v *Int64) uint {
	return 0
}

func EncodeUint8(w io.Writer, id uint16, v *uint8) error {
	return nil
}

func LengthUint8(v *uint8) uint {
	return 0
}

func EncodeUint16(w io.Writer, id uint16, v *uint16) error {
	return nil
}

func LengthUint16(v *uint16) uint {
	return 0
}

func EncodeUint32(w io.Writer, id uint16, v *uint32) error {
	return nil
}

func LengthUint32(v *uint32) uint {
	return 0
}

func EncodeUint64(w io.Writer, id uint16, v *Uint64) error {
	return nil
}

func LengthUint64(v *Uint64) uint {
	return 0
}

func EncodeBytes(w io.Writer, id uint16, v *[]byte) error {
	return nil
}

func LengthBytes(v *[]byte) uint {
	return 0
}

func EncodeString(w io.Writer, id uint16, v *string) error {
	return nil
}

func LengthString(v *string) uint {
	return 0
}

func EncodeTime(w io.Writer, id uint16, v *Time) error {
	return nil
}

func LengthTime(v *Time) uint {
	return 0
}

func EncodeDecimal(w io.Writer, id uint16, v *decimal.Decimal) error {
	return nil
}

func LengthDecimal(v *decimal.Decimal) uint {
	return 0
}

func EncodeBool(w io.Writer, id uint16, v *bool) error {
	return nil
}

func LengthBool(v *bool) uint {
	return 0
}
