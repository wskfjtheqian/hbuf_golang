package hsql

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// NewJson 构造一个 ConverterJson 对象
func NewJson(data any) *ConverterJson {
	return &ConverterJson{
		data: data,
	}
}

// ConverterJson 实现了 sql.Scanner 和 driver.Valuer 接口的自定义类型
type ConverterJson struct {
	data any
}

// Scan 实现了 sql.Scanner 接口的 Scan 方法
func (t *ConverterJson) Scan(value any) error {
	switch v := value.(type) {
	case []byte:
		err := json.Unmarshal(v, t.data)
		if err != nil {
			return err
		}
		return nil
	case string:
		err := json.Unmarshal([]byte(v), t.data)
		if err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("Can't convert %T to DbToJson", value)
}

// Value 实现了 driver.Valuer 接口的 Value 方法
func (t ConverterJson) Value() (driver.Value, error) {
	marshal, err := json.Marshal(t.data)
	if err != nil {
		return nil, err
	}
	return marshal, nil
}

type ConverterStringOption = func(o *ConverterString)

func WithTimeString(format string) ConverterStringOption {
	return func(o *ConverterString) {
		o.timeFormat = format
	}
}

// NewString 构造一个 ConverterString 对象
func NewString(data any, option ...ConverterStringOption) *ConverterString {
	ret := &ConverterString{
		data:       data,
		timeFormat: "2006-01-02 15:04:05.000",
	}
	for _, opt := range option {
		opt(ret)
	}
	return ret
}

// ConverterString 实现了 sql.Scanner 和 driver.Valuer 接口的自定义类型
type ConverterString struct {
	data       any
	timeFormat string
}

// Scan 实现了 sql.Scanner 接口的 Scan 方法
func (t *ConverterString) Scan(value any) error {
	var text string
	switch v := value.(type) {
	case []byte:
		text = string(v)
	case string:
		text = v
	default:
		return fmt.Errorf("Can't convert %T to DbToJson", value)
	}

	typ := reflect.TypeOf(t.data)
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	valueOf := reflect.ValueOf(t.data)
	switch typ.Kind() {
	case reflect.String:
		valueOf.SetString(text)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val, err := strconv.ParseInt(text, 10, valueOf.Type().Bits())
		if err != nil {
			return err
		}
		valueOf.SetInt(val)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val, err := strconv.ParseUint(text, 10, valueOf.Type().Bits())
		if err != nil {
			return err
		}
		valueOf.SetUint(val)
	case reflect.Float32, reflect.Float64:
		val, err := strconv.ParseFloat(text, valueOf.Type().Bits())
		if err != nil {
			return err
		}
		valueOf.SetFloat(val)
	case reflect.Map, reflect.Slice, reflect.Struct:
		err := json.Unmarshal([]byte(text), t.data)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("Can't convert %T to DbToString", value)
	}
	return nil
}

var timeType = reflect.TypeOf(time.Time{})

// Value 实现了 driver.Valuer 接口的 Value 方法
func (t ConverterString) Value() (driver.Value, error) {
	if t.data == nil {
		return nil, nil
	}
	typ := reflect.TypeOf(t.data)
	valueOf := reflect.ValueOf(t.data)
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
		valueOf = valueOf.Elem()
	}
	switch typ.Kind() {
	case reflect.String:
		return valueOf.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(valueOf.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(valueOf.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(valueOf.Float(), 'g', -1, 64), nil
	case reflect.Map, reflect.Slice, reflect.Struct:
		if typ.Kind() == timeType.Kind() {
			val := valueOf.Interface().(time.Time)
			return val.Format(t.timeFormat), nil
		}
		return json.Marshal(t.data)
	default:
		return nil, fmt.Errorf("Can't convert %T to DbToString", t.data)
	}
}
