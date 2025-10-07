package hsql

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
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
