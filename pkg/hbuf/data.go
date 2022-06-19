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
