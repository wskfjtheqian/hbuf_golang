package hutl

import (
	"fmt"
	"strings"
	"time"
)

type TimeZone uint16

// TimeZones 是一组时区字符串，用于生成时区选择器。
var TimeZones = []int32{
	-43200000, // -12:00 International Date Change West
	-39600000, // -11:00 When coordinating the world-11
	-36000000, // -10:00 Alei Islands
	-36000000, // -10:00 Hawaii
	-30600000, // -09:30 Maxus Islands
	-32400000, // -09:00 Alaska
	-32400000, // -09:00 When coordinating the world -09
	-28800000, // -08:00 Pacific Time (the United States and Canada)
	-28800000, // -08:00 Lower Galifa
	-28800000, // -08:00 Coordinating the World -08
	-25200000, // -07:00 Labast, Mazartland
	-25200000, // -07:00 Mountain Time (the United States and Canada)
	-25200000, // -07:00 Arizona
	-25200000, // -07:00 Educate
	-21600000, // -06:00 Easter Island
	-21600000, // -06:00 Guadalahara, Mexico City, Monterey
	-21600000, // -06:00 Saskhawin
	-21600000, // -06:00 Central time (the United States and Canada)
	-21600000, // -06:00 Central America
	-18000000, // -05:00 Boge Da, Lima, Kido, Rio Bronki
	-18000000, // -05:00 Eastern Time (the United States and Canada)
	-18000000, // -05:00 Havana
	-18000000, // -05:00 Haiti
	-18000000, // -05:00 Chermal
	-18000000, // -05:00 Tax and Kaikos Islands
	-18000000, // -05:00 Indian Ana (East) (East)
	-14400000, // -04:00 Atlantic time (Canada)
	-14400000, // -04:00 Galagas
	-14400000, // -04:00 Kaza
	-14400000, // -04:00 George Dun, Rabas, Aids, San Hu'an
	-14400000, // -04:00 San Diego
	-14400000, // -04:00 Yatongsen
	-9000000,  // -03:30 Nuvenovenland
	-10800000, // -03:00 Alagua
	-10800000, // -03:00 Brazilian
	-10800000, // -03:00 Buenos Aires
	-10800000, // -03:00 Cayenne, Fumausa
	-10800000, // -03:00 Montae
	-10800000, // -03:00 Puffa Arenas
	-10800000, // -03:00 Salvador
	-10800000, // -03:00 St. Piel and Michigal Islands
	-7200000,  // -02:00 Greenland
	-7200000,  // -02:00 When coordinating the world -02
	-3600000,  // -01:00 Buddhist islands
	-3600000,  // -01:00 Acel Islands
	0,         // +00:00 When coordinating the world
	0,         // +00:00 Dublin, Edinburgh, Lisbon, London
	0,         // +00:00 Monrovia, Reykjavik
	0,         // +00:00 Shengdomei
	3600000,   // +01:00 Casablanka
	3600000,   // +01:00 Amsterdam, Berlin, Berne, Rome, Stockholm, Vienna
	3600000,   // +01:00 Belgrade, Bladisla, Budapest, Lulburia, Prague
	3600000,   // +01:00 Brussels, Copenhagen, Madrid, Paris
	3600000,   // +01:00 Sarajewo, Skopry, Warsaw, Saglerb
	3600000,   // +01:00 Western China
	7200000,   // +02:00 Berut
	7200000,   // +02:00 Ripari
	7200000,   // +02:00 Harary, Billeria
	7200000,   // +02:00 Helsinki, Kiev, Rica, Sorfiya, Tarin, Vernis
	7200000,   // +02:00 Kichinwu
	7200000,   // +02:00 Kalinrad
	7200000,   // +02:00 Gasha, Helima
	7200000,   // +02:00 Kagums
	7200000,   // +02:00 Cairo
	7200000,   // +02:00 Winhehe
	7200000,   // +02:00 Athens, Buccuster
	7200000,   // +02:00 Jerusalem
	7200000,   // +02:00 Juba
	10800000,  // +03:00 Amman
	10800000,  // +03:00 Baghdad
	10800000,  // +03:00 Damascus
	10800000,  // +03:00 Volgage
	10800000,  // +03:00 Kuwait, Riyadh
	10800000,  // +03:00 Minsk
	10800000,  // +03:00 Moscow, St. Petersburg
	10800000,  // +03:00 Nairobi
	10800000,  // +03:00 Istanbul
	12600000,  // +03:30 Tehran
	14400000,  // +04:00 Abu Dhabi, Maskat
	14400000,  // +04:00 Astraham, Uliyanovsk
	14400000,  // +04:00 Erinewin
	14400000,  // +04:00 Pakugu
	14400000,  // +04:00 Bilis
	14400000,  // +04:00 Louis Port
	14400000,  // +04:00 Salatov
	14400000,  // +04:00 Ilvsk, Samara
	16200000,  // +04:30 Kabul
	18000000,  // +05:00 Ashhabad, Tashgan
	18000000,  // +05:00 Astana
	18000000,  // +05:00 Yekaterinburg
	18000000,  // +05:00 Islamabad, Karachi
	19800000,  // +05:30 Qin Nai, Kolkata, Mumbai, New Delhi
	19800000,  // +05:30 Sriga Wulden Pulala
	20700000,  // +05:45 Kathmandu
	21600000,  // +06:00 Bishkek
	21600000,  // +06:00 Darka
	21600000,  // +06:00 Emuzk
	23400000,  // +06:30 Yangon
	25200000,  // +07:00 Balube, Gorno Altaysk
	25200000,  // +07:00 Kobado
	25200000,  // +07:00 Klasinoelsk
	25200000,  // +07:00 Bangkok, Hanoi, Jakarta
	25200000,  // +07:00 Tomosk
	25200000,  // +07:00 New Siberia
	28800000,  // +08:00 Beijing, Chongqing, Hong Kong Special Administrative Region, Urumqi
	28800000,  // +08:00 Kuala Lumpur, Singapore
	28800000,  // +08:00 Perth
	28800000,  // +08:00 Taipei
	28800000,  // +08:00 Ulanbato
	28800000,  // +08:00 Irkutzk
	31500000,  // +08:45 Ukola
	32400000,  // +09:00 Chita City
	32400000,  // +09:00 Osaka, Sapporo, Tokyo
	32400000,  // +09:00 Pyongyang
	32400000,  // +09:00 Seoul
	32400000,  // +09:00 Yakuzk
	34200000,  // +09:30 Adelaide
	34200000,  // +09:30 Darwin
	36000000,  // +10:00 Brisbane
	36000000,  // +10:00 Vladivostak
	36000000,  // +10:00 Guam, Mogzbi Port
	36000000,  // +10:00 Hobart
	36000000,  // +10:00 Canberra, Melbourne, Sydney
	37800000,  // +10:30 Lord Haojima
	39600000,  // +11:00 Bukkovir Island
	39600000,  // +11:00 Magatan
	39600000,  // +11:00 Norfolk Island
	39600000,  // +11:00 Jacquidh
	39600000,  // +11:00 Sahalin
	39600000,  // +11:00 Solomon Islands, New Cauritonia
	43200000,  // +12:00 Anader, Kanshaga Peter Ravorovsk
	43200000,  // +12:00 Auckland, Wellington
	43200000,  // +12:00 Fiji
	43200000,  // +12:00 When coordinating the world +12
	45900000,  // +12:45 Chartham Islands
	46800000,  // +13:00 Nuku Alpha
	46800000,  // +13:00 Samoa Islands
	46800000,  // +13:00 When coordinating the world +13
	50400000,  // +14:00 Christmas Island
}

func ZoneByOffset(offset int32) *time.Location {
	var name strings.Builder
	if offset < 0 {
		name.WriteString("-")
		offset = -offset
	} else {
		name.WriteString("+")
	}
	offset /= 1000
	h := offset / 3600
	m := (offset % 3600) / 60
	name.WriteString(fmt.Sprintf("%02d:%02d", h, m))

	return time.FixedZone(name.String(), int(offset))
}

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

// StartWeek 返回给定时间所在周的起始日期，周一为起始日期
// 例如: 给定时间为 2021-08-15 12:30:00，则返回 2021-08-09 00:00:00
func StartWeek(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day-int(t.Weekday())-6, 0, 0, 0, 0, t.Location())
}

// EndWeek 返回给定时间所在周的结束日期, 周日为结束日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-08-15 23:59:59
func EndWeek(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day-int(t.Weekday()), 23, 59, 59, 999999999, t.Location())
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
	return time.Date(year, month+1, 0, 23, 59, 59, 999999999, t.Location())
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
	return time.Date(year+1, 1, 0, 23, 59, 59, 999999999, t.Location())
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
	return time.Date(year, quarter*3+1, 0, 23, 59, 59, 999999999, t.Location())
}

// StartHalfYear 返回给定时间所在半年的起始日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-07-01 00:00:00
func StartHalfYear(t time.Time) time.Time {
	year, month, _ := t.Date()
	halfYear := 1
	if month > 6 {
		halfYear = 2
	}
	return time.Date(year, time.Month(halfYear*6-5), 1, 0, 0, 0, 0, t.Location())
}

// EndHalfYear 返回给定时间所在半年的结束日期
// 例如：给定时间为 2021-08-15 12:30:00，则返回 2021-12-31 23:59:59
func EndHalfYear(t time.Time) time.Time {
	year, month, _ := t.Date()
	halfYear := 1
	if month > 6 {
		halfYear = 2
	}
	return time.Date(year, time.Month(halfYear*6)+1, 0, 23, 59, 59, 999999999, t.Location())
}

// IsSameDay 判官是否是同一天
func IsSameDay(t1, t2 time.Time) bool {
	return t1.Year() == t2.Year() && t1.Month() == t2.Month() && t1.Day() == t2.Day()
}

// IsSameWeek 判官是否是同一周
func IsSameWeek(t1, t2 time.Time) bool {
	year1, week1 := t1.ISOWeek()
	year2, week2 := t2.ISOWeek()
	return year1 == year2 && week1 == week2
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
	return t1.UnixMilli()/time.Hour.Milliseconds() == t2.UnixMilli()/time.Hour.Milliseconds()
}

// IsSameMinute 判官是否是同一分钟
func IsSameMinute(t1, t2 time.Time) bool {
	return t1.UnixMilli()/time.Minute.Milliseconds() == t2.UnixMilli()/time.Minute.Milliseconds()
}

// IsSameSecond 判官是否是同一秒
func IsSameSecond(t1, t2 time.Time) bool {
	return t1.UnixMilli()/time.Second.Milliseconds() == t2.UnixMilli()/time.Second.Milliseconds()
}
