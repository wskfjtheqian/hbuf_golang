package hdedup

import (
	"fmt"
	"time"
)

// Day 返回给定时间的日期字符串，格式为 "YYYY-MM-DD"
func Day(t time.Time) string {
	return t.Format("2006-01-02")
}

// Week 返回给定时间的ISO周数字符串，格式为 "YYYY-Www"
func Week(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// Month 返回给定时间的月份字符串，格式为 "YYYY-MM"
func Month(t time.Time) string {
	return t.Format("2006-01")
}
