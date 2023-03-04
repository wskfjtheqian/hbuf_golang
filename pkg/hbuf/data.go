package hbuf

import (
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

func (t *Time) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(time.Time(*t).UnixMilli(), 10)), nil
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

func (t *Int64) MarshalJSON() ([]byte, error) {
	return []byte("\"" + strconv.FormatInt(int64(*t), 10) + "\""), nil
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

func (t *Uint64) MarshalJSON() ([]byte, error) {
	return []byte("\"" + strconv.FormatUint(uint64(*t), 10) + "\""), nil
}
