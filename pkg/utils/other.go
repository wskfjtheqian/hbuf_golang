package utl

import (
	"crypto/md5"
	"encoding/hex"
	"math/rand"
	"reflect"
	"strconv"
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
