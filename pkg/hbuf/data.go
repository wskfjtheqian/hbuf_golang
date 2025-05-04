package hbuf

import (
	"database/sql"
	"database/sql/driver"
	"io"
	"strconv"
	"time"
)

type Description struct {
	Encode func(writer io.Writer, id uint16, v any) error
	Decode func(reader io.Reader, v any) error
	Length func(v any) int
}

func NewFieldDescription[T any](
	encode func(writer io.Writer, id uint16, v *T) error,
	decode func(reader io.Reader, v *T) error,
	length func(v *T) uint,
) *Description {
	return &Description{
		Encode: func(writer io.Writer, id uint16, v any) error {
			return encode(writer, id, v.(*T))
		},
		Decode: func(reader io.Reader, v any) error {
			return decode(reader, v.(*T))
		},
		Length: func(v any) int {
			return int(length(v.(*T)))
		},
	}
}

func NewListDescription[T any](a *Description) *Description {
	return &Description{
		Encode: func(writer io.Writer, id uint16, v any) error {
			l := v.([]T)
			err := EncodeUint16(writer, uint16(len(l)))
			if err != nil {
				return err
			}
			for _, v := range l {
				err = a.Encode(writer, id, v)
				if err != nil {
					return err
				}
			}
			return nil
		},
		Decode: func(reader io.Reader, v any) error {
			l := v.(*[]T)
			n, err := DecodeUint16(reader)
			if err != nil {
				return err
			}
			*l = make([]T, n)
			for i := 0; i < int(n); i++ {
				err = a.Decode(reader, &(*l)[i])
				if err != nil {
					return err
				}
			}
			return nil
		},
		Length: func(v any) int {
			l := v.([]T)
			lLen := 2
			for _, v := range l {
				lLen += a.Length(v)
			}
			return lLen
		},
	}
}

func NewMapDescription[K comparable, V any](a *Description, b *Description) *Description {
	return &Description{
		Encode: func(writer io.Writer, id uint16, v any) error {
			m := v.(map[K]V)
			for k, v := range m {
				err := a.Encode(writer, id, k)
				if err != nil {
					return err
				}
				err = b.Encode(writer, id, v)
				if err != nil {
					return err
				}
			}
			return nil
		},
		Decode: func(reader io.Reader, v any) error {
			m := v.(*map[K]V)
			for {
				k := K{}
				err := a.Decode(reader, &k)
				if err == io.EOF {
					break
				}
				if err != nil {
					return err
				}
				v := V{}
				err = b.Decode(reader, &v)
				if err != nil {
					return err
				}
				(*m)[k] = v
			}
			return nil
		},
		Length: func(v any) int {
			m := v.(map[K]V)
			l := 0
			for k, v := range m {
				l += a.Length(k) + b.Length(v)
			}
			return l
		},
	}
}

type Data interface {
	Description() map[uint16]*Description
}

type Unmarshaler interface {
	UnmarshalHBuf([]byte) error
}

type Marshaler interface {
	MarshalHBuf() ([]byte, error)
}

type Int64 int64

type Uint64 uint64

type Time time.Time

func (t *Time) UnmarshalJSON(data []byte) error {
	parseInt, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return err
	}

	*t = Time(time.UnixMilli(parseInt))
	return nil
}

func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(time.Time(t).UnixMilli(), 10)), nil
}

func (t *Time) Scan(value any) error {
	nullTime := sql.NullTime{}
	err := nullTime.Scan(value)
	if err != nil {
		return err
	}
	if nullTime.Valid {
		*t = Time(nullTime.Time)
	}
	return nil
}

func (t Time) Value() (driver.Value, error) {
	return time.Time(t), nil
}
