package utl

import "time"

func EqualDay(val1 time.Time, val2 time.Time) bool {
	return val1.Year() == val2.Year() && val1.Month() == val2.Month() && val1.Day() == val2.Day()
}

func EqualMonth(val1 time.Time, val2 time.Time) bool {
	return val1.Year() == val2.Year() && val1.Month() == val2.Month()
}

func EqualYear(val1 time.Time, val2 time.Time) bool {
	return val1.Year() == val2.Year()
}

func EqualHour(val1 time.Time, val2 time.Time) bool {
	return val1.UnixNano()/int64(time.Hour) == val2.UnixNano()/int64(time.Hour)
}

func EqualMinute(val1 time.Time, val2 time.Time) bool {
	return val1.UnixNano()/int64(time.Minute) == val2.UnixNano()/int64(time.Minute)
}

func EqualSecond(val1 time.Time, val2 time.Time) bool {
	return val1.UnixNano()/int64(time.Second) == val2.UnixNano()/int64(time.Second)
}

func EqualMillisecond(val1 time.Time, val2 time.Time) bool {
	return val1.UnixNano()/int64(time.Millisecond) == val2.UnixNano()/int64(time.Millisecond)
}
