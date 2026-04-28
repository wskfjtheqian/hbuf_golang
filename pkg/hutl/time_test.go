package hutl

import (
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

// 将星期一设置为第一天
func TestTimeFunctions(t *testing.T) {
	// 创建一个基准时间
	baseTime := time.Date(2021, 8, 15, 12, 30, 0, 0, time.Local)

	// 测试 StartHour 函数
	startHour := htime.StartHour(baseTime)
	expectedStartHour := time.Date(2021, 8, 15, 12, 0, 0, 0, time.Local)
	if !startHour.Equal(expectedStartHour) {
		t.Errorf("StartHour failed: expected %v, got %v", expectedStartHour, startHour)
	}

	// 测试 EndHour 函数
	endHour := htime.EndHour(baseTime)
	expectedEndHour := time.Date(2021, 8, 15, 12, 59, 59, 999999999, time.Local)
	if !endHour.Equal(expectedEndHour) {
		t.Errorf("EndHour failed: expected %v, got %v", expectedEndHour, endHour)
	}

	// 测试 StartMinute 函数
	startMinute := htime.StartMinute(baseTime)
	expectedStartMinute := baseTime
	if !startMinute.Equal(expectedStartMinute) {
		t.Errorf("StartMinute failed: expected %v, got %v", expectedStartMinute, startMinute)
	}

	// 测试 EndMinute 函数
	endMinute := htime.EndMinute(baseTime)
	expectedEndMinute := time.Date(2021, 8, 15, 12, 30, 59, 999999999, time.Local)
	if !endMinute.Equal(expectedEndMinute) {
		t.Errorf("EndMinute failed: expected %v, got %v", expectedEndMinute, endMinute)
	}

	// 测试 StartDay 函数
	startDay := htime.StartDay(baseTime)
	expectedStartDay := time.Date(2021, 8, 15, 0, 0, 0, 0, time.Local)
	if !startDay.Equal(expectedStartDay) {
		t.Errorf("StartDay failed: expected %v, got %v", expectedStartDay, startDay)
	}

	// 测试 EndDay 函数
	endDay := htime.EndDay(baseTime)
	expectedEndDay := time.Date(2021, 8, 15, 23, 59, 59, 999999999, time.Local)
	if !endDay.Equal(expectedEndDay) {
		t.Errorf("EndDay failed: expected %v, got %v", expectedEndDay, endDay)
	}

	// 测试 StartWeek 函数
	startWeek := htime.StartWeek(baseTime)
	expectedStartWeek := time.Date(2021, 8, 9, 0, 0, 0, 0, time.Local) // 本周的周一
	if !startWeek.Equal(expectedStartWeek) {
		t.Errorf("StartWeek failed: expected %v, got %v", expectedStartWeek, startWeek)
	}

	// 测试 EndWeek 函数
	endWeek := htime.EndWeek(baseTime)
	expectedEndWeek := time.Date(2021, 8, 15, 23, 59, 59, 999999999, time.Local) // 本周的周日
	if !endWeek.Equal(expectedEndWeek) {
		t.Errorf("EndWeek failed: expected %v, got %v", expectedEndWeek, endWeek)
	}

	// 测试 StartMonth 函数
	startMonth := htime.StartMonth(baseTime)
	expectedStartMonth := time.Date(2021, 8, 1, 0, 0, 0, 0, time.Local)
	if !startMonth.Equal(expectedStartMonth) {
		t.Errorf("StartMonth failed: expected %v, got %v", expectedStartMonth, startMonth)
	}

	// 测试 EndMonth 函数
	endMonth := htime.EndMonth(baseTime)
	expectedEndMonth := time.Date(2021, 8, 31, 23, 59, 59, 999999999, time.Local)
	if !endMonth.Equal(expectedEndMonth) {
		t.Errorf("EndMonth failed: expected %v, got %v", expectedEndMonth, endMonth)
	}

	// 测试 StartYear 函数
	startYear := htime.StartYear(baseTime)
	expectedStartYear := time.Date(2021, 1, 1, 0, 0, 0, 0, time.Local)
	if !startYear.Equal(expectedStartYear) {
		t.Errorf("StartYear failed: expected %v, got %v", expectedStartYear, startYear)
	}

	// 测试 EndYear 函数
	endYear := htime.EndYear(baseTime)
	expectedEndYear := time.Date(2021, 12, 31, 23, 59, 59, 999999999, time.Local)
	if !endYear.Equal(expectedEndYear) {
		t.Errorf("EndYear failed: expected %v, got %v", expectedEndYear, endYear)
	}

	// 测试 StartQuarter 函数
	startQuarter := htime.StartQuarter(baseTime)
	expectedStartQuarter := time.Date(2021, 7, 1, 0, 0, 0, 0, time.Local)
	if !startQuarter.Equal(expectedStartQuarter) {
		t.Errorf("StartQuarter failed: expected %v, got %v", expectedStartQuarter, startQuarter)
	}

	// 测试 EndQuarter 函数
	endQuarter := htime.EndQuarter(baseTime)
	expectedEndQuarter := time.Date(2021, 9, 30, 23, 59, 59, 999999999, time.Local)
	if !endQuarter.Equal(expectedEndQuarter) {
		t.Errorf("EndQuarter failed: expected %v, got %v", expectedEndQuarter, endQuarter)
	}

	// 测试 StartHalfYear 函数
	startHalfYear := htime.StartHalfYear(baseTime)
	expectedStartHalfYear := time.Date(2021, 7, 1, 0, 0, 0, 0, time.Local)
	if !startHalfYear.Equal(expectedStartHalfYear) {
		t.Errorf("StartHalfYear failed: expected %v, got %v", expectedStartHalfYear, startHalfYear)
	}

	// 测试 EndHalfYear 函数
	endHalfYear := htime.EndHalfYear(baseTime)
	expectedEndHalfYear := time.Date(2021, 12, 31, 23, 59, 59, 999999999, time.Local)
	if !endHalfYear.Equal(expectedEndHalfYear) {
		t.Errorf("EndHalfYear failed: expected %v, got %v", expectedEndHalfYear, endHalfYear)
	}

	// 测试 IsSameDay 函数
	isSameDay := htime.IsSameDay(baseTime, baseTime)
	if !isSameDay {
		t.Error("IsSameDay failed: expected true, got false")
	}

	// 边界情况：不同日期
	otherDay := time.Date(2021, 8, 16, 12, 30, 0, 0, time.Local)
	if htime.IsSameDay(baseTime, otherDay) {
		t.Error("IsSameDay failed: expected false, got true")
	}

	// 测试 IsSameWeek 函数
	isSameWeek := htime.IsSameWeek(baseTime, baseTime)
	if !isSameWeek {
		t.Error("IsSameWeek failed: expected true, got false")
	}

	// 边界情况：不同周
	otherWeek := time.Date(2021, 8, 22, 12, 30, 0, 0, time.Local) // 周日
	if htime.IsSameWeek(baseTime, otherWeek) {
		t.Error("IsSameWeek failed: expected false, got true")
	}

	// 测试 IsSameMonth 函数
	isSameMonth := htime.IsSameMonth(baseTime, baseTime)
	if !isSameMonth {
		t.Error("IsSameMonth failed: expected true, got false")
	}

	// 边界情况：不同月
	otherMonth := time.Date(2021, 9, 1, 12, 30, 0, 0, time.Local)
	if htime.IsSameMonth(baseTime, otherMonth) {
		t.Error("IsSameMonth failed: expected false, got true")
	}

	// 测试 IsSameYear 函数
	isSameYear := htime.IsSameYear(baseTime, baseTime)
	if !isSameYear {
		t.Error("IsSameYear failed: expected true, got false")
	}

	// 边界情况：不同年
	otherYear := time.Date(2022, 8, 15, 12, 30, 0, 0, time.Local)
	if htime.IsSameYear(baseTime, otherYear) {
		t.Error("IsSameYear failed: expected false, got true")
	}

	// 测试 IsSameQuarter 函数
	isSameQuarter := htime.IsSameQuarter(baseTime, baseTime)
	if !isSameQuarter {
		t.Error("IsSameQuarter failed: expected true, got false")
	}

	// 边界情况：不同季度
	otherQuarter := time.Date(2021, 10, 1, 12, 30, 0, 0, time.Local)
	if htime.IsSameQuarter(baseTime, otherQuarter) {
		t.Error("IsSameQuarter failed: expected false, got true")
	}

	// 测试 IsSameHalfYear 函数
	isSameHalfYear := htime.IsSameHalfYear(baseTime, baseTime)
	if !isSameHalfYear {
		t.Error("IsSameHalfYear failed: expected true, got false")
	}

	// 边界情况：不同半年
	otherHalfYear := time.Date(2022, 1, 1, 12, 30, 0, 0, time.Local)
	if htime.IsSameHalfYear(baseTime, otherHalfYear) {
		t.Error("IsSameHalfYear failed: expected false, got true")
	}

	// 测试 IsSameHour 函数
	isSameHour := htime.IsSameHour(baseTime, baseTime)
	if !isSameHour {
		t.Error("IsSameHour failed: expected true, got false")
	}

	// 边界情况：不同小时
	otherHour := time.Date(2021, 8, 15, 13, 30, 0, 0, time.Local)
	if htime.IsSameHour(baseTime, otherHour) {
		t.Error("IsSameHour failed: expected false, got true")
	}

	// 测试 IsSameMinute 函数
	isSameMinute := htime.IsSameMinute(baseTime, baseTime)
	if !isSameMinute {
		t.Error("IsSameMinute failed: expected true, got false")
	}

	// 边界情况：不同分钟
	otherMinute := time.Date(2021, 8, 15, 12, 31, 0, 0, time.Local)
	if htime.IsSameMinute(baseTime, otherMinute) {
		t.Error("IsSameMinute failed: expected false, got true")
	}

	// 测试 IsSameSecond 函数
	isSameSecond := htime.IsSameSecond(baseTime, baseTime)
	if !isSameSecond {
		t.Error("IsSameSecond failed: expected true, got false")
	}

	// 边界情况：不同秒
	otherSecond := time.Date(2021, 8, 15, 12, 30, 1, 0, time.Local)
	if htime.IsSameSecond(baseTime, otherSecond) {
		t.Error("IsSameSecond failed: expected false, got true")
	}
}
