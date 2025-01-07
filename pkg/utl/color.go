package utl

import "strings"

// Black 返回一个黑色的字符串
// 效果：\u001B[0;30m黑色字符串\u001B[0m
func Black(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;30m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// Red 返回一个红色的字符串
// 效果：\u001B[0;31m红色字符串\u001B[0m
func Red(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;31m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// Green 返回一个绿色的字符串
// 效果：\u001B[0;32m绿色字符串\u001B[0m
func Green(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;32m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// Yellow 返回一个黄色的字符串
// 效果：\u001B[0;33m黄色字符串\u001B[0m
func Yellow(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;33m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// Blue 返回一个蓝色的字符串
// 效果：\u001B[0;34m蓝色字符串\u001B[0m
func Blue(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;34m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// Magenta 返回一个品红色的字符串
// 效果：\u001B[0;35m品红色字符串\u001B[0m
func Magenta(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;35m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// Cyan 返回一个青色的字符串
// 效果：\u001B[0;36m青色字符串\u001B[0m
func Cyan(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;36m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// White 返回一个白色的字符串
// 效果：\u001B[0;37m白色字符串\u001B[0m
func White(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;37m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BoldBlack 返回一个加粗的黑色的字符串
// 效果：\u001B[1;30m加粗黑色字符串\u001B[0m
func BoldBlack(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[1;30m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BoldRed 返回一个加粗的红色的字符串
// 效果：\u001B[1;31m加粗红色字符串\u001B[0m
func BoldRed(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[1;31m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BoldGreen 返回一个加粗的绿色的字符串
// 效果：\u001B[1;32m加粗绿色字符串\u001B[0m
func BoldGreen(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[1;32m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BoldYellow 返回一个加粗的黄色的字符串
// 效果：\u001B[1;33m加粗黄色字符串\u001B[0m
func BoldYellow(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[1;33m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BoldBlue 返回一个加粗的蓝色的字符串
// 效果：\u001B[1;34m加粗蓝色字符串\u001B[0m
func BoldBlue(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[1;34m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BoldMagenta 返回一个加粗的品红色的字符串
// 效果：\u001B[1;35m加粗品红色字符串\u001B[0m
func BoldMagenta(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[1;35m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BoldCyan 返回一个加粗的青色的字符串
// 效果：\u001B[1;36m加粗青色字符串\u001B[0m
func BoldCyan(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[1;36m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BoldWhite 返回一个加粗的白色的字符串
// 效果：\u001B[1;37m加粗白色字符串\u001B[0m
func BoldWhite(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[1;37m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ItalicBlack 返回一个斜体的黑色的字符串
// 效果：\u001B[3;30m斜体黑色字符串\u001B[0m
func ItalicBlack(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[3;30m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ItalicRed 返回一个斜体的红色的字符串
// 效果：\u001B[3;31m斜体红色字符串\u001B[0m
func ItalicRed(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[3;31m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ItalicGreen 返回一个斜体的绿色的字符串
// 效果：\u001B[3;32m斜体绿色字符串\u001B[0m
func ItalicGreen(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[3;32m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ItalicYellow 返回一个斜体的黄色的字符串
// 效果：\u001B[3;33m斜体黄色字符串\u001B[0m
func ItalicYellow(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[3;33m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ItalicBlue 返回一个斜体的蓝色的字符串
// 效果：\u001B[3;34m斜体蓝色字符串\u001B[0m
func ItalicBlue(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[3;34m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ItalicMagenta 返回一个斜体的品红色的字符串
// 效果：\u001B[3;35m斜体品红色字符串\u001B[0m
func ItalicMagenta(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[3;35m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ItalicCyan 返回一个斜体的青色的字符串
// 效果：\u001B[3;36m斜体青色字符串\u001B[0m
func ItalicCyan(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[3;36m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ItalicWhite 返回一个斜体的白色的字符串
// 效果：\u001B[3;37m斜体白色字符串\u001B[0m
func ItalicWhite(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[3;37m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// UnderlineBlack 返回一个下划线的黑色的字符串
// 效果：\u001B[4;30m下划线黑色字符串\u001B[0m
func UnderlineBlack(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[4;30m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// UnderlineRed 返回一个下划线的红色的字符串
// 效果：\u001B[4;31m下划线红色字符串\u001B[0m
func UnderlineRed(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[4;31m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// UnderlineGreen 返回一个下划线的绿色的字符串
// 效果：\u001B[4;32m下划线绿色字符串\u001B[0m
func UnderlineGreen(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[4;32m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// UnderlineYellow 返回一个下划线的黄色的字符串
// 效果：\u001B[4;33m下划线黄色字符串\u001B[0m
func UnderlineYellow(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[4;33m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// UnderlineBlue 返回一个下划线的蓝色的字符串
// 效果：\u001B[4;34m下划线蓝色字符串\u001B[0m
func UnderlineBlue(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[4;34m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// UnderlineMagenta 返回一个下划线的品红色的字符串
// 效果：\u001B[4;35m下划线品红色字符串\u001B[0m
func UnderlineMagenta(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[4;35m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// UnderlineCyan 返回一个下划线的青色的字符串
// 效果：\u001B[4;36m下划线青色字符串\u001B[0m
func UnderlineCyan(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[4;36m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// UnderlineWhite 返回一个下划线的白色的字符串
// 效果：\u001B[4;37m下划线白色字符串\u001B[0m
func UnderlineWhite(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[4;37m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BlinkBlack 返回一个闪烁的黑色的字符串
// 效果：\u001B[5;30m闪烁黑色字符串\u001B[0m
func BlinkBlack(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[5;30m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BlinkRed 返回一个闪烁的红色的字符串
// 效果：\u001B[5;31m闪烁红色字符串\u001B[0m
func BlinkRed(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[5;31m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BlinkGreen 返回一个闪烁的绿色的字符串
// 效果：\u001B[5;32m闪烁绿色字符串\u001B[0m
func BlinkGreen(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[5;32m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BlinkYellow 返回一个闪烁的黄色的字符串
// 效果：\u001B[5;33m闪烁黄色字符串\u001B[0m
func BlinkYellow(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[5;33m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BlinkBlue 返回一个闪烁的蓝色的字符串
// 效果：\u001B[5;34m闪烁蓝色字符串\u001B[0m
func BlinkBlue(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[5;34m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BlinkMagenta 返回一个闪烁的品红色的字符串
// 效果：\u001B[5;35m闪烁品红色字符串\u001B[0m
func BlinkMagenta(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[5;35m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BlinkCyan 返回一个闪烁的青色的字符串
// 效果：\u001B[5;36m闪烁青色字符串\u001B[0m
func BlinkCyan(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[5;36m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// BlinkWhite 返回一个闪烁的白色的字符串
// 效果：\u001B[5;37m闪烁白色字符串\u001B[0m
func BlinkWhite(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[5;37m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// InverseBlack 返回一个反相的黑色的字符串
// 效果：\u001B[7;30m反相黑色字符串\u001B[0m
func InverseBlack(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;30m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// InverseRed 返回一个反相的红色的字符串
// 效果：\u001B[7;31m反相红色字符串\u001B[0m
func InverseRed(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;31m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// InverseGreen 返回一个反相的绿色的字符串
// 效果：\u001B[7;32m反相绿色字符串\u001B[0m
func InverseGreen(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;32m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// InverseYellow 返回一个反相的黄色的字符串
// 效果：\u001B[7;33m反相黄色字符串\u001B[0m
func InverseYellow(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;33m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// InverseBlue 返回一个反相的蓝色的字符串
// 效果：\u001B[7;34m反相蓝色字符串\u001B[0m
func InverseBlue(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;34m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// InverseMagenta 返回一个反相的品红色的字符串
// 效果：\u001B[7;35m反相品红色字符串\u001B[0m
func InverseMagenta(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;35m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// InverseCyan 返回一个反相的青色的字符串
// 效果：\u001B[7;36m反相青色字符串\u001B[0m
func InverseCyan(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;36m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// InverseWhite 返回一个反相的白色的字符串
// 效果：\u001B[7;37m反相白色字符串\u001B[0m
func InverseWhite(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;37m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// FaintBlack 返回一个变暗的黑色的字符串
// 效果：\u001B[2;30m变暗黑色字符串\u001B[0m
func FaintBlack(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[2;30m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// FaintRed 返回一个变暗的红色的字符串
// 效果：\u001B[2;31m变暗红色字符串\u001B[0m
func FaintRed(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[2;31m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// FaintGreen 返回一个变暗的绿色的字符串
// 效果：\u001B[2;32m变暗绿色字符串\u001B[0m
func FaintGreen(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[2;32m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// FaintYellow 返回一个变暗的黄色的字符串
// 效果：\u001B[2;33m变暗黄色字符串\u001B[0m
func FaintYellow(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[2;33m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// FaintBlue 返回一个变暗的蓝色的字符串
// 效果：\u001B[2;34m变暗蓝色字符串\u001B[0m
func FaintBlue(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[2;34m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// FaintMagenta 返回一个变暗的品红色的字符串
// 效果：\u001B[2;35m变暗品红色字符串\u001B[0m
func FaintMagenta(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[2;35m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// FaintCyan 返回一个变暗的青色的字符串
// 效果：\u001B[2;36m变暗青色字符串\u001B[0m
func FaintCyan(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[2;36m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// FaintWhite 返回一个变暗的白色的字符串
// 效果：\u001B[2;37m变暗白色字符串\u001B[0m
func FaintWhite(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[2;37m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ReverseBlack 返回一个反转的黑色的字符串
// 效果：\u001B[7;30m反转黑色字符串\u001B[0m
func ReverseBlack(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;30m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ReverseRed 返回一个反转的红色的字符串
// 效果：\u001B[7;31m反转红色字符串\u001B[0m
func ReverseRed(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;31m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ReverseGreen 返回一个反转的绿色的字符串
// 效果：\u001B[7;32m反转绿色字符串\u001B[0m
func ReverseGreen(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;32m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ReverseYellow 返回一个反转的黄色的字符串
// 效果：\u001B[7;33m反转黄色字符串\u001B[0m
func ReverseYellow(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;33m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ReverseBlue 返回一个反转的蓝色的字符串
// 效果：\u001B[7;34m反转蓝色字符串\u001B[0m
func ReverseBlue(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;34m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ReverseMagenta 返回一个反转的品红色的字符串
// 效果：\u001B[7;35m反转品红色字符串\u001B[0m
func ReverseMagenta(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;35m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ReverseCyan 返回一个反转的青色的字符串
// 效果：\u001B[7;36m反转青色字符串\u001B[0m
func ReverseCyan(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;36m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

// ReverseWhite 返回一个反转的白色的字符串
// 效果：\u001B[7;37m反转白色字符串\u001B[0m
func ReverseWhite(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[7;37m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}
