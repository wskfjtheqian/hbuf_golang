package hbuf

import (
	"database/sql"
	"database/sql/driver"
	"reflect"
	"strconv"
	"time"
)

type Type byte

const (
	TInt = iota + 0
	TUint
	TFloat
	TBool
	TBytes
	TList
	TMap
	TData
)

type Int64 int64

func (i Int64) String() string {
	return strconv.FormatInt(int64(i), 10)
}

type Uint64 uint64

func (i Uint64) String() string {
	return strconv.FormatUint(uint64(i), 10)
}

type Time time.Time

func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(time.Time(t).UnixMilli(), 10)), nil
}

func (t *Time) UnmarshalJSON(data []byte) error {
	parseInt, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return err
	}
	*t = Time(time.UnixMilli(parseInt))
	return nil
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

type Data interface {
	Descriptors() Descriptor
}

func Marshal(data Data, tag string) ([]byte, error) {
	buf := make([]byte, 0, 128)
	return data.Descriptors().Encode(buf, reflect.ValueOf(data).UnsafePointer(), nil, tag), nil
}

func Unmarshal(buf []byte, data Data, tag string) (err error) {
	if len(buf) == 0 {
		return nil
	}
	//defer func() {
	//	if r := recover(); r!= nil {
	//		err = r.(error)
	//	}
	//}()
	typ, _, valueLen, buf := DecodeType(buf)
	_, err = data.Descriptors().Decode(buf, reflect.ValueOf(data).UnsafePointer(), typ, true, valueLen, tag)
	if err != nil {
		return err
	}
	return nil
}
