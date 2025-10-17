package hutl

import "time"

type TimeZone int

const (
	TimeZoneInternationalDateChangeWest                                  = 0
	TimeZoneWhencoordinatingtheworld_11                                  = 1
	TimeZoneAleiIslands                                                  = 2
	TimeZoneHawaii                                                       = 3
	TimeZoneMaxusIslands                                                 = 4
	TimeZoneAlaska                                                       = 5
	TimeZoneWhencoordinatingtheworld_09                                  = 6
	TimeZonePacificTimetheUnitedStatesandCanada                          = 7
	TimeZoneLowerGalifa                                                  = 8
	TimeZoneCoordinatingtheWorld_08                                      = 9
	TimeZoneLabast_Mazartland                                            = 10
	TimeZoneMountainTimetheUnitedStatesandCanada                         = 11
	TimeZoneArizona                                                      = 12
	TimeZoneEducate                                                      = 13
	TimeZoneEasterIsland                                                 = 14
	TimeZoneGuadalahara_MexicoCity_Monterey                              = 15
	TimeZoneSaskhawin                                                    = 16
	TimeZoneCentraltimetheUnitedStatesandCanada                          = 17
	TimeZoneCentralAmerica                                               = 18
	TimeZoneBogeDa_Lima_Kido_RioBronki                                   = 19
	TimeZoneEasternTimetheUnitedStatesandCanada                          = 20
	TimeZoneHavana                                                       = 21
	TimeZoneHaiti                                                        = 22
	TimeZoneChermal                                                      = 23
	TimeZoneTaxandKaikosIslands                                          = 24
	TimeZoneIndianAnaEastEast                                            = 25
	TimeZoneAtlantictimeCanada                                           = 26
	TimeZoneGalagas                                                      = 27
	TimeZoneKaza                                                         = 28
	TimeZoneGeorgeDun_Rabas_Aids_SanHuan                                 = 29
	TimeZoneSanDiego                                                     = 30
	TimeZoneYatongsen                                                    = 31
	TimeZoneNuvenovenland                                                = 32
	TimeZoneAlagua                                                       = 33
	TimeZoneBrazilian                                                    = 34
	TimeZoneBuenosAires                                                  = 35
	TimeZoneCayenne_Fumausa                                              = 36
	TimeZoneMontae                                                       = 37
	TimeZonePuffaArenas                                                  = 38
	TimeZoneSalvador                                                     = 39
	TimeZoneStPielandMichigalIslands                                     = 40
	TimeZoneGreenland                                                    = 41
	TimeZoneWhencoordinatingtheworld_02                                  = 42
	TimeZoneBuddhistislands                                              = 43
	TimeZoneAcelIslands                                                  = 44
	TimeZoneWhencoordinatingtheworld                                     = 45
	TimeZoneDublin_Edinburgh_Lisbon_London                               = 46
	TimeZoneMonrovia_Reykjavik                                           = 47
	TimeZoneShengdomei                                                   = 48
	TimeZoneCasablanka                                                   = 49
	TimeZoneAmsterdam_Berlin_Berne_Rome_Stockholm_Vienna                 = 50
	TimeZoneBelgrade_Bladisla_Budapest_Lulburia_Prague                   = 51
	TimeZoneBrussels_Copenhagen_Madrid_Paris                             = 52
	TimeZoneSarajewo_Skopry_Warsaw_Saglerb                               = 53
	TimeZoneWesternChina                                                 = 54
	TimeZoneBerut                                                        = 55
	TimeZoneRipari                                                       = 56
	TimeZoneHarary_Billeria                                              = 57
	TimeZoneHelsinki_Kiev_Rica_Sorfiya_Tarin_Vernis                      = 58
	TimeZoneKichinwu                                                     = 59
	TimeZoneKalinrad                                                     = 60
	TimeZoneGasha_Helima                                                 = 61
	TimeZoneKagums                                                       = 62
	TimeZoneCairo                                                        = 63
	TimeZoneWinhehe                                                      = 64
	TimeZoneAthens_Buccuster                                             = 65
	TimeZoneJerusalem                                                    = 66
	TimeZoneJuba                                                         = 67
	TimeZoneAmman                                                        = 68
	TimeZoneBaghdad                                                      = 69
	TimeZoneDamascus                                                     = 70
	TimeZoneVolgage                                                      = 71
	TimeZoneKuwait_Riyadh                                                = 72
	TimeZoneMinsk                                                        = 73
	TimeZoneMoscow_StPetersburg                                          = 74
	TimeZoneNairobi                                                      = 75
	TimeZoneIstanbul                                                     = 76
	TimeZoneTehran                                                       = 77
	TimeZoneAbuDhabi_Maskat                                              = 78
	TimeZoneAstraham_Uliyanovsk                                          = 79
	TimeZoneErinewin                                                     = 80
	TimeZonePakugu                                                       = 81
	TimeZoneBilis                                                        = 82
	TimeZoneLouisPort                                                    = 83
	TimeZoneSalatov                                                      = 84
	TimeZoneIlvsk_Samara                                                 = 85
	TimeZoneKabul                                                        = 86
	TimeZoneAshhabad_Tashgan                                             = 87
	TimeZoneAstana                                                       = 88
	TimeZoneYekaterinburg                                                = 89
	TimeZoneIslamabad_Karachi                                            = 90
	TimeZoneQinNai_Kolkata_Mumbai_NewDelhi                               = 91
	TimeZoneSrigaWuldenPulala                                            = 92
	TimeZoneKathmandu                                                    = 93
	TimeZoneBishkek                                                      = 94
	TimeZoneDarka                                                        = 95
	TimeZoneEmuzk                                                        = 96
	TimeZoneYangon                                                       = 97
	TimeZoneBalube_GornoAltaysk                                          = 98
	TimeZoneKobado                                                       = 99
	TimeZoneKlasinoelsk                                                  = 100
	TimeZoneBangkok_Hanoi_Jakarta                                        = 101
	TimeZoneTomosk                                                       = 102
	TimeZoneNewSiberia                                                   = 103
	TimeZoneBeijing_Chongqing_HongKongSpecialAdministrativeRegion_Urumqi = 104
	TimeZoneKualaLumpur_Singapore                                        = 105
	TimeZonePerth                                                        = 106
	TimeZoneTaipei                                                       = 107
	TimeZoneUlanbato                                                     = 108
	TimeZoneIrkutzk                                                      = 109
	TimeZoneUkola                                                        = 110
	TimeZoneChitaCity                                                    = 111
	TimeZoneOsaka_Sapporo_Tokyo                                          = 112
	TimeZonePyongyang                                                    = 113
	TimeZoneSeoul                                                        = 114
	TimeZoneYakuzk                                                       = 115
	TimeZoneAdelaide                                                     = 116
	TimeZoneDarwin                                                       = 117
	TimeZoneBrisbane                                                     = 118
	TimeZoneVladivostak                                                  = 119
	TimeZoneGuam_MogzbiPort                                              = 120
	TimeZoneHobart                                                       = 121
	TimeZoneCanberra_Melbourne_Sydney                                    = 122
	TimeZoneLordHaojima                                                  = 123
	TimeZoneBukkovirIsland                                               = 124
	TimeZoneMagatan                                                      = 125
	TimeZoneNorfolkIsland                                                = 126
	TimeZoneJacquidh                                                     = 127
	TimeZoneSahalin                                                      = 128
	TimeZoneSolomonIslands_NewCauritonia                                 = 129
	TimeZoneAnader_KanshagaPeterRavorovsk                                = 130
	TimeZoneAuckland_Wellington                                          = 131
	TimeZoneFiji                                                         = 132
	TimeZoneWhencoordinatingtheworld_12                                  = 133
	TimeZoneCharthamIslands                                              = 134
	TimeZoneNukuAlpha                                                    = 135
	TimeZoneSamoaIslands                                                 = 136
	TimeZoneWhencoordinatingtheworld_13                                  = 137
	TimeZoneChristmasIsland                                              = 138
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

var TimeZoneOffset = map[string]int{
	"-12:00": -43200,
	"-11:00": -39600,
	"-10:00": -36000,
	"-09:00": -32400,
	"-09:30": -30600,
	"-08:00": -28800,
	"-07:00": -25200,
	"-06:00": -21600,
	"-05:00": -18000,
	"-04:00": -14400,
	"-03:00": -10800,
	"-03:30": -9000,
	"-02:00": -7200,
	"-01:00": -3600,
	"+00:00": 0,
	"+01:00": 3600,
	"+02:00": 7200,
	"+03:00": 10800,
	"+03:30": 12600,
	"+04:00": 14400,
	"+04:30": 16200,
	"+05:00": 18000,
	"+05:30": 19800,
	"+05:45": 20700,
	"+06:00": 21600,
	"+06:30": 23400,
	"+07:00": 25200,
	"+08:00": 28800,
	"+08:45": 31500,
	"+09:00": 32400,
	"+09:30": 34200,
	"+10:00": 36000,
	"+10:30": 37800,
	"+11:00": 39600,
	"+12:00": 43200,
	"+12:45": 45900,
	"+13:00": 46800,
	"+14:00": 50400,
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
