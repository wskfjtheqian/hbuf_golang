package hbuf

import (
	"reflect"
	"strconv"
	"strings"
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
	str := time.Time(t).Format("2006-01-02T15:04:05.999Z07:00")
	return []byte("\"" + str + "\""), nil
}

func (t *Time) UnmarshalJSON(data []byte) error {
	parse, err := time.Parse("2006-01-02T15:04:05.999Z07:00", strings.Trim(string(data), "\""))
	if err != nil {
		return err
	}
	*t = Time(parse)
	return nil
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
