package utl

import "time"

func IsSameDay(val1 time.Time, val2 time.Time) bool {
	return val1.Year() == val2.Year() && val1.Month() == val2.Month() && val1.Day() == val2.Day()
}

func IsSameMonth(val1 time.Time, val2 time.Time) bool {
	return val1.Year() == val2.Year() && val1.Month() == val2.Month()
}

func IsSameYear(val1 time.Time, val2 time.Time) bool {
	return val1.Year() == val2.Year()
}

func IsSameHour(val1 time.Time, val2 time.Time) bool {
	return val1.UnixNano()/int64(time.Hour) == val2.UnixNano()/int64(time.Hour)
}

func IsSameMinute(val1 time.Time, val2 time.Time) bool {
	return val1.UnixNano()/int64(time.Minute) == val2.UnixNano()/int64(time.Minute)
}

func IsSameSecond(val1 time.Time, val2 time.Time) bool {
	return val1.UnixNano()/int64(time.Second) == val2.UnixNano()/int64(time.Second)
}

func IsSameMillisecond(val1 time.Time, val2 time.Time) bool {
	return val1.UnixNano()/int64(time.Millisecond) == val2.UnixNano()/int64(time.Millisecond)
}
