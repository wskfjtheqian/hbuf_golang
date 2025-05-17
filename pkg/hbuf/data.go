package hbuf

import (
	"reflect"
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

type Uint64 uint64

type Time time.Time

func (t Time) MarshalJSON() ([]byte, error) {
	//return []byte(strconv.FormatInt(time.Time(t).UnixMilli(), 10)), nil
	return time.Time(t).MarshalJSON()
}

func (t *Time) UnmarshalJSON(data []byte) error {
	//parseInt, err := strconv.ParseInt(string(data), 10, 64)
	//if err != nil {
	//	return err
	//}
	//*t = Time(time.UnixMilli(parseInt))
	//return nil
	return (*time.Time)(t).UnmarshalJSON(data)
}

type Data interface {
	Descriptors() Descriptor
}

func Marshal(data Data, tag string) ([]byte, error) {
	buf := make([]byte, 0, 128)
	return data.Descriptors().Encode(buf, reflect.ValueOf(data).UnsafePointer(), 0, false, tag), nil
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
	typ, _, valueLen, buf := ReaderTypeId(buf)
	_, err = data.Descriptors().Decode(buf, reflect.ValueOf(data).UnsafePointer(), typ, valueLen, tag)
	if err != nil {
		return err
	}
	return nil
}
