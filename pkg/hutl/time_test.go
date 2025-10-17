package hutl

import (
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 将星期一设置为第一天
func TestTimeFunctions(t *testing.T) {
	// 创建一个基准时间
	baseTime := time.Date(2021, 8, 15, 12, 30, 0, 0, time.Local)

	// 测试 StartHour 函数
	startHour := StartHour(baseTime)
	expectedStartHour := time.Date(2021, 8, 15, 12, 0, 0, 0, time.Local)
	if !startHour.Equal(expectedStartHour) {
		t.Errorf("StartHour failed: expected %v, got %v", expectedStartHour, startHour)
	}

	// 测试 EndHour 函数
	endHour := EndHour(baseTime)
	expectedEndHour := time.Date(2021, 8, 15, 12, 59, 59, 999999999, time.Local)
	if !endHour.Equal(expectedEndHour) {
		t.Errorf("EndHour failed: expected %v, got %v", expectedEndHour, endHour)
	}

	// 测试 StartMinute 函数
	startMinute := StartMinute(baseTime)
	expectedStartMinute := baseTime
	if !startMinute.Equal(expectedStartMinute) {
		t.Errorf("StartMinute failed: expected %v, got %v", expectedStartMinute, startMinute)
	}

	// 测试 EndMinute 函数
	endMinute := EndMinute(baseTime)
	expectedEndMinute := time.Date(2021, 8, 15, 12, 30, 59, 999999999, time.Local)
	if !endMinute.Equal(expectedEndMinute) {
		t.Errorf("EndMinute failed: expected %v, got %v", expectedEndMinute, endMinute)
	}

	// 测试 StartDay 函数
	startDay := StartDay(baseTime)
	expectedStartDay := time.Date(2021, 8, 15, 0, 0, 0, 0, time.Local)
	if !startDay.Equal(expectedStartDay) {
		t.Errorf("StartDay failed: expected %v, got %v", expectedStartDay, startDay)
	}

	// 测试 EndDay 函数
	endDay := EndDay(baseTime)
	expectedEndDay := time.Date(2021, 8, 15, 23, 59, 59, 999999999, time.Local)
	if !endDay.Equal(expectedEndDay) {
		t.Errorf("EndDay failed: expected %v, got %v", expectedEndDay, endDay)
	}

	// 测试 StartWeek 函数
	startWeek := StartWeek(baseTime)
	expectedStartWeek := time.Date(2021, 8, 9, 0, 0, 0, 0, time.Local) // 本周的周一
	if !startWeek.Equal(expectedStartWeek) {
		t.Errorf("StartWeek failed: expected %v, got %v", expectedStartWeek, startWeek)
	}

	// 测试 EndWeek 函数
	endWeek := EndWeek(baseTime)
	expectedEndWeek := time.Date(2021, 8, 15, 23, 59, 59, 999999999, time.Local) // 本周的周日
	if !endWeek.Equal(expectedEndWeek) {
		t.Errorf("EndWeek failed: expected %v, got %v", expectedEndWeek, endWeek)
	}

	// 测试 StartMonth 函数
	startMonth := StartMonth(baseTime)
	expectedStartMonth := time.Date(2021, 8, 1, 0, 0, 0, 0, time.Local)
	if !startMonth.Equal(expectedStartMonth) {
		t.Errorf("StartMonth failed: expected %v, got %v", expectedStartMonth, startMonth)
	}

	// 测试 EndMonth 函数
	endMonth := EndMonth(baseTime)
	expectedEndMonth := time.Date(2021, 8, 31, 23, 59, 59, 999999999, time.Local)
	if !endMonth.Equal(expectedEndMonth) {
		t.Errorf("EndMonth failed: expected %v, got %v", expectedEndMonth, endMonth)
	}

	// 测试 StartYear 函数
	startYear := StartYear(baseTime)
	expectedStartYear := time.Date(2021, 1, 1, 0, 0, 0, 0, time.Local)
	if !startYear.Equal(expectedStartYear) {
		t.Errorf("StartYear failed: expected %v, got %v", expectedStartYear, startYear)
	}

	// 测试 EndYear 函数
	endYear := EndYear(baseTime)
	expectedEndYear := time.Date(2021, 12, 31, 23, 59, 59, 999999999, time.Local)
	if !endYear.Equal(expectedEndYear) {
		t.Errorf("EndYear failed: expected %v, got %v", expectedEndYear, endYear)
	}

	// 测试 StartQuarter 函数
	startQuarter := StartQuarter(baseTime)
	expectedStartQuarter := time.Date(2021, 7, 1, 0, 0, 0, 0, time.Local)
	if !startQuarter.Equal(expectedStartQuarter) {
		t.Errorf("StartQuarter failed: expected %v, got %v", expectedStartQuarter, startQuarter)
	}

	// 测试 EndQuarter 函数
	endQuarter := EndQuarter(baseTime)
	expectedEndQuarter := time.Date(2021, 9, 30, 23, 59, 59, 999999999, time.Local)
	if !endQuarter.Equal(expectedEndQuarter) {
		t.Errorf("EndQuarter failed: expected %v, got %v", expectedEndQuarter, endQuarter)
	}

	// 测试 StartHalfYear 函数
	startHalfYear := StartHalfYear(baseTime)
	expectedStartHalfYear := time.Date(2021, 7, 1, 0, 0, 0, 0, time.Local)
	if !startHalfYear.Equal(expectedStartHalfYear) {
		t.Errorf("StartHalfYear failed: expected %v, got %v", expectedStartHalfYear, startHalfYear)
	}

	// 测试 EndHalfYear 函数
	endHalfYear := EndHalfYear(baseTime)
	expectedEndHalfYear := time.Date(2021, 12, 31, 23, 59, 59, 999999999, time.Local)
	if !endHalfYear.Equal(expectedEndHalfYear) {
		t.Errorf("EndHalfYear failed: expected %v, got %v", expectedEndHalfYear, endHalfYear)
	}

	// 测试 IsSameDay 函数
	isSameDay := IsSameDay(baseTime, baseTime)
	if !isSameDay {
		t.Error("IsSameDay failed: expected true, got false")
	}

	// 边界情况：不同日期
	otherDay := time.Date(2021, 8, 16, 12, 30, 0, 0, time.Local)
	if IsSameDay(baseTime, otherDay) {
		t.Error("IsSameDay failed: expected false, got true")
	}

	// 测试 IsSameWeek 函数
	isSameWeek := IsSameWeek(baseTime, baseTime)
	if !isSameWeek {
		t.Error("IsSameWeek failed: expected true, got false")
	}

	// 边界情况：不同周
	otherWeek := time.Date(2021, 8, 22, 12, 30, 0, 0, time.Local) // 周日
	if IsSameWeek(baseTime, otherWeek) {
		t.Error("IsSameWeek failed: expected false, got true")
	}

	// 测试 IsSameMonth 函数
	isSameMonth := IsSameMonth(baseTime, baseTime)
	if !isSameMonth {
		t.Error("IsSameMonth failed: expected true, got false")
	}

	// 边界情况：不同月
	otherMonth := time.Date(2021, 9, 1, 12, 30, 0, 0, time.Local)
	if IsSameMonth(baseTime, otherMonth) {
		t.Error("IsSameMonth failed: expected false, got true")
	}

	// 测试 IsSameYear 函数
	isSameYear := IsSameYear(baseTime, baseTime)
	if !isSameYear {
		t.Error("IsSameYear failed: expected true, got false")
	}

	// 边界情况：不同年
	otherYear := time.Date(2022, 8, 15, 12, 30, 0, 0, time.Local)
	if IsSameYear(baseTime, otherYear) {
		t.Error("IsSameYear failed: expected false, got true")
	}

	// 测试 IsSameQuarter 函数
	isSameQuarter := IsSameQuarter(baseTime, baseTime)
	if !isSameQuarter {
		t.Error("IsSameQuarter failed: expected true, got false")
	}

	// 边界情况：不同季度
	otherQuarter := time.Date(2021, 10, 1, 12, 30, 0, 0, time.Local)
	if IsSameQuarter(baseTime, otherQuarter) {
		t.Error("IsSameQuarter failed: expected false, got true")
	}

	// 测试 IsSameHalfYear 函数
	isSameHalfYear := IsSameHalfYear(baseTime, baseTime)
	if !isSameHalfYear {
		t.Error("IsSameHalfYear failed: expected true, got false")
	}

	// 边界情况：不同半年
	otherHalfYear := time.Date(2022, 1, 1, 12, 30, 0, 0, time.Local)
	if IsSameHalfYear(baseTime, otherHalfYear) {
		t.Error("IsSameHalfYear failed: expected false, got true")
	}

	// 测试 IsSameHour 函数
	isSameHour := IsSameHour(baseTime, baseTime)
	if !isSameHour {
		t.Error("IsSameHour failed: expected true, got false")
	}

	// 边界情况：不同小时
	otherHour := time.Date(2021, 8, 15, 13, 30, 0, 0, time.Local)
	if IsSameHour(baseTime, otherHour) {
		t.Error("IsSameHour failed: expected false, got true")
	}

	// 测试 IsSameMinute 函数
	isSameMinute := IsSameMinute(baseTime, baseTime)
	if !isSameMinute {
		t.Error("IsSameMinute failed: expected true, got false")
	}

	// 边界情况：不同分钟
	otherMinute := time.Date(2021, 8, 15, 12, 31, 0, 0, time.Local)
	if IsSameMinute(baseTime, otherMinute) {
		t.Error("IsSameMinute failed: expected false, got true")
	}

	// 测试 IsSameSecond 函数
	isSameSecond := IsSameSecond(baseTime, baseTime)
	if !isSameSecond {
		t.Error("IsSameSecond failed: expected true, got false")
	}

	// 边界情况：不同秒
	otherSecond := time.Date(2021, 8, 15, 12, 30, 1, 0, time.Local)
	if IsSameSecond(baseTime, otherSecond) {
		t.Error("IsSameSecond failed: expected false, got true")
	}
}

func TestTimeConst(t *testing.T) {
	aa := map[string]string{
		"0":   "International Date Change West",
		"1":   "When coordinating the world-11",
		"10":  "Labast, Mazartland",
		"100": "Klasinoelsk",
		"101": "Bangkok, Hanoi, Jakarta",
		"102": "Tomosk",
		"103": "New Siberia",
		"104": "Beijing, Chongqing, Hong Kong Special Administrative Region, Urumqi",
		"105": "Kuala Lumpur, Singapore",
		"106": "Perth",
		"107": "Taipei",
		"108": "Ulanbato",
		"109": "Irkutzk",
		"11":  "Mountain Time (the United States and Canada)",
		"110": "Ukola",
		"111": "Chita City",
		"112": "Osaka, Sapporo, Tokyo",
		"113": "Pyongyang",
		"114": "Seoul",
		"115": "Yakuzk",
		"116": "Adelaide",
		"117": "Darwin",
		"118": "Brisbane",
		"119": "Vladivostak",
		"12":  "Arizona",
		"120": "Guam, Mogzbi Port",
		"121": "Hobart",
		"122": "Canberra, Melbourne, Sydney",
		"123": "Lord Haojima",
		"124": "Bukkovir Island",
		"125": "Magatan",
		"126": "Norfolk Island",
		"127": "Jacquidh",
		"128": "Sahalin",
		"129": "Solomon Islands, New Cauritonia",
		"13":  "Educate",
		"130": "Anader, Kanshaga Peter Ravorovsk",
		"131": "Auckland, Wellington",
		"132": "Fiji",
		"133": "When coordinating the world +12",
		"134": "Chartham Islands",
		"135": "Nuku Alpha",
		"136": "Samoa Islands",
		"137": "When coordinating the world +13",
		"138": "Christmas Island",
		"14":  "Easter Island",
		"15":  "Guadalahara, Mexico City, Monterey",
		"16":  "Saskhawin",
		"17":  "Central time (the United States and Canada)",
		"18":  "Central America",
		"19":  "Boge Da, Lima, Kido, Rio Bronki",
		"2":   "Alei Islands",
		"20":  "Eastern Time (the United States and Canada)",
		"21":  "Havana",
		"22":  "Haiti",
		"23":  "Chermal",
		"24":  "Tax and Kaikos Islands",
		"25":  "Indian Ana (East) (East)",
		"26":  "Atlantic time (Canada)",
		"27":  "Galagas",
		"28":  "Kaza",
		"29":  "George Dun, Rabas, Aids, San Hu'an",
		"3":   "Hawaii",
		"30":  "San Diego",
		"31":  "Yatongsen",
		"32":  "Nuvenovenland",
		"33":  "Alagua",
		"34":  "Brazilian",
		"35":  "Buenos Aires",
		"36":  "Cayenne, Fumausa",
		"37":  "Montae",
		"38":  "Puffa Arenas",
		"39":  "Salvador",
		"4":   "Maxus Islands",
		"40":  "St. Piel and Michigal Islands",
		"41":  "Greenland",
		"42":  "When coordinating the world -02",
		"43":  "Buddhist islands",
		"44":  "Acel Islands",
		"45":  "When coordinating the world",
		"46":  "Dublin, Edinburgh, Lisbon, London",
		"47":  "Monrovia, Reykjavik",
		"48":  "Shengdomei",
		"49":  "Casablanka",
		"5":   "Alaska",
		"50":  "Amsterdam, Berlin, Berne, Rome, Stockholm, Vienna",
		"51":  "Belgrade, Bladisla, Budapest, Lulburia, Prague",
		"52":  "Brussels, Copenhagen, Madrid, Paris",
		"53":  "Sarajewo, Skopry, Warsaw, Saglerb",
		"54":  "Western China",
		"55":  "Berut",
		"56":  "Ripari",
		"57":  "Harary, Billeria",
		"58":  "Helsinki, Kiev, Rica, Sorfiya, Tarin, Vernis",
		"59":  "Kichinwu",
		"6":   "When coordinating the world -09",
		"60":  "Kalinrad",
		"61":  "Gasha, Helima",
		"62":  "Kagums",
		"63":  "Cairo",
		"64":  "Winhehe",
		"65":  "Athens, Buccuster",
		"66":  "Jerusalem",
		"67":  "Juba",
		"68":  "Amman",
		"69":  "Baghdad",
		"7":   "Pacific Time (the United States and Canada)",
		"70":  "Damascus",
		"71":  "Volgage",
		"72":  "Kuwait, Riyadh",
		"73":  "Minsk",
		"74":  "Moscow, St. Petersburg",
		"75":  "Nairobi",
		"76":  "Istanbul",
		"77":  "Tehran",
		"78":  "Abu Dhabi, Maskat",
		"79":  "Astraham, Uliyanovsk",
		"8":   "Lower Galifa",
		"80":  "Erinewin",
		"81":  "Pakugu",
		"82":  "Bilis",
		"83":  "Louis Port",
		"84":  "Salatov",
		"85":  "Ilvsk, Samara",
		"86":  "Kabul",
		"87":  "Ashhabad, Tashgan",
		"88":  "Astana",
		"89":  "Yekaterinburg",
		"9":   "Coordinating the World -08",
		"90":  "Islamabad, Karachi",
		"91":  "Qin Nai, Kolkata, Mumbai, New Delhi",
		"92":  "Sriga Wulden Pulala",
		"93":  "Kathmandu",
		"94":  "Bishkek",
		"95":  "Darka",
		"96":  "Emuzk",
		"97":  "Yangon",
		"98":  "Balube, Gorno Altaysk",
		"99":  "Kobado",
	}

	keys := Slice(Keys(aa), func(t string) int {
		val, _ := strconv.Atoi(t)
		return val
	})
	sort.Ints(keys)
	for _, key := range keys {
		val := aa[strconv.Itoa(key)]
		val = strings.ReplaceAll(val, " ", "")
		val = strings.ReplaceAll(val, ",", "_")
		val = strings.ReplaceAll(val, "(", "")
		val = strings.ReplaceAll(val, ")", "")
		val = strings.ReplaceAll(val, "-", "_")
		println("TimeZone" + val + "=" + strconv.Itoa(key))
	}
}
