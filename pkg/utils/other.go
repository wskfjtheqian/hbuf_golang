package utl

import (
	"crypto/md5"
	"encoding/hex"
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
		if 0 != i && (0 < len(old) && "/" != old[len(old)-1:]) && (0 < len(item) && "/" != item[:1]) {
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

// 转换对象
func T[F any, E any](val E, call func(val E) F) F {
	var temp any = val
	rv := reflect.ValueOf(temp)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		var noop F
		return noop
	}
	return call(val)
}

// 转换列表
func TList[F any, E any](list []E, call func(val E) F) []F {
	if nil == list {
		return []F{}
	}
	field := make([]F, len(list))
	for i, item := range list {
		field[i] = call(item)
	}
	return field
}

type SliceType interface {
	int | uint | int8 | uint8 | int16 | uint16 | int32 | uint32 | int64 | uint64 | float32 | float64 | string | bool |
		*int | *uint | *int8 | *uint8 | *int16 | *uint16 | *int32 | *uint32 | *int64 | *uint64 | *float32 | *float64 | *string | *bool |
		chan int | chan uint | chan int8 | chan uint8 | chan int16 | chan uint16 | chan int32 | chan uint32 | chan int64 | chan uint64 | chan float32 | chan float64 | chan string | chan bool |
		chan *int | chan *uint | chan *int8 | chan *uint8 | chan *int16 | chan *uint16 | chan *int32 | chan *uint32 | chan *int64 | chan *uint64 | chan *float32 | chan *float64 | chan *string | chan *bool
}

// 转换映射
func TMap[K SliceType, F any, E any](maps map[K]E, call func(val E) F) map[K]F {
	fields := make(map[K]F)
	if nil == maps {
		return fields
	}
	for i, item := range maps {
		rv := reflect.ValueOf(item)
		if rv.Kind() == reflect.Pointer && rv.IsNil() {
			continue
		}
		fields[i] = call(item)
	}
	return fields
}

// 列表转换为映射
func ListTMap[K SliceType, F any, E any](list []E, call func(val E) (K, F)) map[K]F {
	fields := make(map[K]F)
	if nil == list {
		return fields
	}
	for _, item := range list {
		key, value := call(item)
		fields[key] = value
	}
	return fields
}

// 映射转换为列表
func MapTList[K SliceType, F any, E any](maps map[K]F, call func(key K, val F) E) []E {
	if nil == maps {
		return []E{}
	}
	fields := make([]E, len(maps))
	i := 0
	for key, item := range maps {
		value := call(key, item)
		fields[i] = value
		i++
	}
	return fields
}

// 转换为指针
func TPointer[E any](val E) *E {
	return &val
}
