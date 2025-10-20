package utl

import (
	"encoding/base64"
	"testing"
)

func TestEncrypt(t *testing.T) {
	key, err := base64.StdEncoding.DecodeString("dYQSAWHDSTryebSD0lG1BgQUysbsEnG3+tAERc5i7zk=")
	if err != nil {
		return
	}
	data, err := AesEncryptCBC([]byte("com.apk.application.demo1"), key)
	if err != nil {
		return
	}
	cbc, err := AesDecryptCBC(data, key)
	if err != nil {
		return
	}
	println(string(cbc))
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

func TestZoneByOffset(t *testing.T) {
	for k, v := range TimeZoneOffset {

		t.Log(k, v)
		t.Log(ZoneByOffset(v))
	}
}
