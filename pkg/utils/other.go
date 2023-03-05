package utl

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func Md5(data []byte) string {
	temp := md5.Sum(data)
	return hex.EncodeToString(temp[:])
}

func RandMd5() string {
	data := md5.Sum([]byte(strconv.FormatInt(time.Now().UnixMilli(), 10) + strconv.FormatInt(rand.Int63(), 10)))
	return hex.EncodeToString(data[:])
}

func IsNil(i any) bool {
	defer func() {
		recover()
	}()
	return reflect.ValueOf(i).IsNil()
}

func UrlJoin(elem ...string) string {
	text := strings.Builder{}
	old := ""
	for i, item := range elem {
		if 0 != i && "/" != old[len(old)-1:] && (0 < len(item) && "/" != item[:1]) {
			text.WriteString("/")
		}
		old = item
		text.WriteString(item)
	}
	return text.String()
}

func ToAnyList[T any](l []T) []any {
	ret := make([]any, len(l))
	for i, v := range l {
		ret[i] = v
	}
	return ret
}

func ToQuestions[T any](l []T, question string) string {
	ret := strings.Builder{}
	for i, _ := range l {
		if 0 != i {
			ret.WriteString(question)
			ret.WriteString(" ")
		}
		ret.WriteString("?")
	}
	return ret.String()
}

func ToPointer[T any](l T) *T {
	return &l
}

const (
	textBlack = iota + 30
	textRed
	textGreen
	textYellow
	textBlue
	textPurple
	textCyan
	textWhite
)

func Black(str string) string {
	return textColor(textBlack, str)
}

func Red(str string) string {
	return textColor(textRed, str)
}
func Yellow(str string) string {
	return textColor(textYellow, str)
}
func Green(str string) string {
	return textColor(textGreen, str)
}
func Cyan(str string) string {
	return textColor(textCyan, str)
}
func Blue(str string) string {
	return textColor(textBlue, str)
}
func Purple(str string) string {
	return textColor(textPurple, str)
}
func White(str string) string {
	return textColor(textWhite, str)
}

func textColor(color int, str string) string {
	return fmt.Sprintf("\x1b[0;%dm%s\x1b[0m", color, str)
}
