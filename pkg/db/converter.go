package db

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

type ConverterJson struct {
	data any
}

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

func (t ConverterJson) Value() (driver.Value, error) {
	marshal, err := json.Marshal(t.data)
	if err != nil {
		return nil, err
	}
	return marshal, nil
}

func NewJson(data any) *ConverterJson {
	return &ConverterJson{
		data: data,
	}
}
