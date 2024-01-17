package utl

import "time"

func IsSameDay(val1 time.Time, val2 time.Time) bool {
	return val1.Year() == val2.Year() && val1.Month() == val2.Month() && val1.Day() == val2.Day()
}
