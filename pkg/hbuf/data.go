package hbuf

import (
	"database/sql"
	"database/sql/driver"
	"strconv"
	"time"
)

type Data interface {
	ToData() ([]byte, error)

	FormData([]byte) error
}

type Time struct {
	Time time.Time
}

func (t *Time) UnmarshalJSON(data []byte) error {
	parseInt, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return err
	}
	t.Time = time.UnixMilli(parseInt)
	return nil
}

func (t *Time) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(t.Time.UnixMilli(), 10)), nil
}

// Scan implements the Scanner interface.
func (t *Time) Scan(value interface{}) error {
	nullTime := sql.NullTime{}
	err := nullTime.Scan(value)
	if err != nil {
		return err
	}
	if nullTime.Valid {
		t.Time = nullTime.Time
	}
	return nil
}

// Value implements the driver Valuer interface.
func (t Time) Value() (driver.Value, error) {
	return t.Time, nil
}

type Int64 struct {
	Val int64
}

func (t *Int64) UnmarshalJSON(data []byte) error {
	parseInt, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return err
	}
	t.Val = parseInt
	return nil
}

func (t *Int64) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(t.Val, 10)), nil
}

// Scan implements the Scanner interface.
func (t *Int64) Scan(value interface{}) error {
	nullInt64 := sql.NullInt64{}
	err := nullInt64.Scan(value)
	if err != nil {
		return err
	}
	if nullInt64.Valid {
		t.Val = nullInt64.Int64
	}
	return nil
}

// Value implements the driver Valuer interface.
func (t Int64) Value() (driver.Value, error) {
	return t.Val, nil
}

type Uint64 struct {
	Val uint64
}

func (t *Uint64) UnmarshalJSON(data []byte) error {
	parseInt, err := strconv.ParseUint(string(data), 10, 64)
	if err != nil {
		return err
	}
	t.Val = parseInt
	return nil
}

func (t *Uint64) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatUint(t.Val, 10)), nil
}

// Scan implements the Scanner interface.
func (t *Uint64) Scan(value interface{}) error {
	nullUint64 := sql.NullInt64{}
	err := nullUint64.Scan(value)
	if err != nil {
		return err
	}
	if nullUint64.Valid {
		t.Val = uint64(nullUint64.Int64)
	}
	return nil
}

// Value implements the driver Valuer interface.
func (t Uint64) Value() (driver.Value, error) {
	return int64(t.Val), nil
}
