package utl

import (
	"fmt"
	"strings"
	"time"
)

// TimeZones 是一组时区字符串，用于生成时区选择器。
var TimeZones = []string{
	"-12:00",
	"-11:00",
	"-10:00",
	"-10:00",
	"-09:30",
	"-09:00",
	"-09:00",
	"-08:00",
	"-08:00",
	"-08:00",
	"-07:00",
	"-07:00",
	"-07:00",
	"-07:00",
	"-06:00",
	"-06:00",
	"-06:00",
	"-06:00",
	"-06:00",
	"-05:00",
	"-05:00",
	"-05:00",
	"-05:00",
	"-05:00",
	"-05:00",
	"-05:00",
	"-04:00",
	"-04:00",
	"-04:00",
	"-04:00",
	"-04:00",
	"-04:00",
	"-03:30",
	"-03:00",
	"-03:00",
	"-03:00",
	"-03:00",
	"-03:00",
	"-03:00",
	"-03:00",
	"-03:00",
	"-02:00",
	"-02:00",
	"-01:00",
	"-01:00",
	"+00:00",
	"+00:00",
	"+00:00",
	"+00:00",
	"+01:00",
	"+01:00",
	"+01:00",
	"+01:00",
	"+01:00",
	"+01:00",
	"+02:00",
	"+02:00",
	"+02:00",
	"+02:00",
	"+02:00",
	"+02:00",
	"+02:00",
	"+02:00",
	"+02:00",
	"+02:00",
	"+02:00",
	"+02:00",
	"+02:00",
	"+03:00",
	"+03:00",
	"+03:00",
	"+03:00",
	"+03:00",
	"+03:00",
	"+03:00",
	"+03:00",
	"+03:00",
	"+03:30",
	"+04:00",
	"+04:00",
	"+04:00",
	"+04:00",
	"+04:00",
	"+04:00",
	"+04:00",
	"+04:00",
	"+04:30",
	"+05:00",
	"+05:00",
	"+05:00",
	"+05:00",
	"+05:30",
	"+05:30",
	"+05:45",
	"+06:00",
	"+06:00",
	"+06:00",
	"+06:30",
	"+07:00",
	"+07:00",
	"+07:00",
	"+07:00",
	"+07:00",
	"+07:00",
	"+08:00",
	"+08:00",
	"+08:00",
	"+08:00",
	"+08:00",
	"+08:00",
	"+08:45",
	"+09:00",
	"+09:00",
	"+09:00",
	"+09:00",
	"+09:00",
	"+09:30",
	"+09:30",
	"+10:00",
	"+10:00",
	"+10:00",
	"+10:00",
	"+10:00",
	"+10:30",
	"+11:00",
	"+11:00",
	"+11:00",
	"+11:00",
	"+11:00",
	"+11:00",
	"+12:00",
	"+12:00",
	"+12:00",
	"+12:00",
	"+12:45",
	"+13:00",
	"+13:00",
	"+13:00",
	"+14:00",
}

func ZoneByOffset(offset int) *time.Location {
	var name strings.Builder
	if offset < 0 {
		name.WriteString("-")
		offset = -offset
	} else {
		name.WriteString("+")
	}

	h := offset / 3600000
	m := (offset % 3600) / 60000
	name.WriteString(fmt.Sprintf("%02d:%02d", h, m))

	return time.FixedZone(name.String(), offset)
}

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
