package utl

import "time"

// StartHour 返回给定时间所在小时的起始日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-08-15 00:00:00
func StartHour(t time.Time) time.Time {
	year, month, day := t.Date()
	hour, _, _ := t.Clock()
	return time.Date(year, month, day, hour, 0, 0, 0, t.Location())
}

// EndHour 返回给定时间所在小时的结束日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-08-15 12:59:59
func EndHour(t time.Time) time.Time {
	year, month, day := t.Date()
	hour, _, _ := t.Clock()
	return time.Date(year, month, day, hour, 59, 59, 999999999, t.Location())
}

// StartMinute 返回给定时间所在分钟的起始日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-08-15 12:30:00
func StartMinute(t time.Time) time.Time {
	year, month, day := t.Date()
	hour, minute, _ := t.Clock()
	return time.Date(year, month, day, hour, minute, 0, 0, t.Location())
}

// EndMinute 返回给定时间所在分钟的结束日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-08-15 12:30:59
func EndMinute(t time.Time) time.Time {
	year, month, day := t.Date()
	hour, minute, _ := t.Clock()
	return time.Date(year, month, day, hour, minute, 59, 999999999, t.Location())
}

// StartDay 返回给定时间所在天的起始日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-08-15 00:00:00
func StartDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

// EndDay 返回给定时间所在天的结束日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-08-15 23:59:59
func EndDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 23, 59, 59, 999999999, t.Location())
}

// StartWeek 返回给定时间所在周的起始日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-08-12 00:00:00
func StartWeek(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day-int(t.Weekday()), 0, 0, 0, 0, t.Location())
}

// EndWeek 返回给定时间所在周的结束日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-08-18 23:59:59
func EndWeek(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day-int(t.Weekday())+6, 23, 59, 59, 999999999, t.Location())
}

// StartMonth 返回给定时间所在月的起始日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-08-01 00:00:00
func StartMonth(t time.Time) time.Time {
	year, month, _ := t.Date()
	return time.Date(year, month, 1, 0, 0, 0, 0, t.Location())
}

// EndMonth 返回给定时间所在月的结束日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-08-31 23:59:59
func EndMonth(t time.Time) time.Time {
	year, month, _ := t.Date()
	return time.Date(year, month+1, 1, 0, 0, 0, 0, t.Location()).AddDate(0, 0, -1)
}

// StartYear 返回给定时间所在年的起始日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-01-01 00:00:00
func StartYear(t time.Time) time.Time {
	year, _, _ := t.Date()
	return time.Date(year, 1, 1, 0, 0, 0, 0, t.Location())
}

// EndYear 返回给定时间所在年的结束日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-12-31 23:59:59
func EndYear(t time.Time) time.Time {
	year, _, _ := t.Date()
	return time.Date(year+1, 1, 1, 0, 0, 0, 0, t.Location()).AddDate(-1, 0, 0)
}

// StartQuarter 返回给定时间所在季度的起始日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-07-01 00:00:00
func StartQuarter(t time.Time) time.Time {
	year, month, _ := t.Date()
	quarter := (month-1)/3 + 1
	return time.Date(year, (quarter-1)*3+1, 1, 0, 0, 0, 0, t.Location())
}

// EndQuarter 返回给定时间所在季度的结束日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-09-30 23:59:59
func EndQuarter(t time.Time) time.Time {
	year, month, _ := t.Date()
	quarter := (month-1)/3 + 1
	return time.Date(year, quarter*3, 1, 0, 0, 0, 0, t.Location()).AddDate(0, 3, -1)
}

// StartHalfYear 返回给定时间所在半年的起始日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-07-01 00:00:00
func StartHalfYear(t time.Time) time.Time {
	year, month, _ := t.Date()
	halfYear := 1
	if month > 6 {
		halfYear = 2
	}
	return time.Date(year, time.Month(halfYear*6-6), 1, 0, 0, 0, 0, t.Location())
}

// EndHalfYear 返回给定时间所在半年的结束日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-12-31 23:59:59
func EndHalfYear(t time.Time) time.Time {
	year, month, _ := t.Date()
	halfYear := 1
	if month > 6 {
		halfYear = 2
	}
	return time.Date(year, time.Month(halfYear*6), 1, 0, 0, 0, 0, t.Location()).AddDate(0, 6, -1)
}

// IsSameDay 判官是否是同一天
func IsSameDay(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year() && t1.Month() == t2.Month() && t1.Day() == t2.Day()
}

// IsSameWeek 判官是否是同一周
func IsSameWeek(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year() && t1.Weekday() == t2.Weekday()
}

// IsSameMonth 判官是否是同一月
func IsSameMonth(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year() && t1.Month() == t2.Month()
}

// IsSameYear 判官是否是同一年
func IsSameYear(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year()
}

// IsSameQuarter 判官是否是同一季度
func IsSameQuarter(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year() && (t1.Month()-1)/3 == (t2.Month()-1)/3
}

// IsSameHalfYear 判官是否是同一半年
func IsSameHalfYear(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year() && (t1.Month()-1)/6 == (t2.Month()-1)/6
}

// IsSameHour 判官是否是同一时刻
func IsSameHour(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year() && t1.Month() == t2.Month() && t1.Day() == t2.Day() && t1.Hour() == t2.Hour()
}

// IsSameMinute 判官是否是同一分钟
func IsSameMinute(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year() && t1.Month() == t2.Month() && t1.Day() == t2.Day() && t1.Hour() == t2.Hour() && t1.Minute() == t2.Minute()
}

// IsSameSecond 判官是否是同一秒
func IsSameSecond(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year() && t1.Month() == t2.Month() && t1.Day() == t2.Day() && t1.Hour() == t2.Hour() && t1.Minute() == t2.Minute() && t1.Second() == t2.Second()
}
