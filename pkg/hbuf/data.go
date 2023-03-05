package hbuf

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strconv"
	"time"
)

type Data interface {
	ToData() ([]byte, error)

	FormData([]byte) error
}

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

type Int64 int64

func unquoteIfQuoted(value any) (string, error) {
	var bytes []byte

	switch v := value.(type) {
	case string:
		bytes = []byte(v)
	case []byte:
		bytes = v
	default:
		return "", fmt.Errorf("could not convert value '%+v' to byte array of type '%T'",
			value, value)
	}

	// If the amount is quoted, strip the quotes
	if len(bytes) > 2 && bytes[0] == '"' && bytes[len(bytes)-1] == '"' {
		bytes = bytes[1 : len(bytes)-1]
	}
	return string(bytes), nil
}

func (t *Int64) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	str, err := unquoteIfQuoted(data)
	if err != nil {
		return fmt.Errorf("error decoding string '%s': %s", data, err)
	}

	parseInt, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return err
	}
	*t = Int64(parseInt)
	return nil
}

func (t Int64) MarshalJSON() ([]byte, error) {
	return []byte("\"" + strconv.FormatInt(int64(t), 10) + "\""), nil
}

func (t *Int64) Scan(value any) error {
	nullInt64 := sql.NullInt64{}
	err := nullInt64.Scan(value)
	if err != nil {
		return err
	}
	if nullInt64.Valid {
		*t = Int64(nullInt64.Int64)
	}
	return nil
}

func (t Int64) Value() (driver.Value, error) {
	return int64(t), nil
}

type Uint64 uint64

func (t *Uint64) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	str, err := unquoteIfQuoted(data)
	if err != nil {
		return fmt.Errorf("error decoding string '%s': %s", data, err)
	}

	parseInt, err := strconv.ParseUint(str, 10, 64)
	if err != nil {
		return err
	}
	*t = Uint64(parseInt)
	return nil
}

func (t Uint64) MarshalJSON() ([]byte, error) {
	return []byte("\"" + strconv.FormatUint(uint64(t), 10) + "\""), nil
}

func (t *Uint64) Scan(value any) error {
	nullUint64 := sql.NullInt64{}
	err := nullUint64.Scan(value)
	if err != nil {
		return err
	}
	if nullUint64.Valid {
		*t = Uint64(nullUint64.Int64)
	}
	return nil
}

func (t Uint64) Value() (driver.Value, error) {
	return int64(t), nil
}
