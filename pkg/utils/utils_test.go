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
	"-12:00": -43200000,
	"-11:00": -39600000,
	"-10:00": -36000000,
	"-09:00": -32400000,
	"-09:30": -30600000,
	"-08:00": -28800000,
	"-07:00": -25200000,
	"-06:00": -21600000,
	"-05:00": -18000000,
	"-04:00": -14400000,
	"-03:00": -10800000,
	"-03:30": -9000000,
	"-02:00": -7200000,
	"-01:00": -3600000,
	"+00:00": 0000,
	"+01:00": 3600000,
	"+02:00": 7200000,
	"+03:00": 10800000,
	"+03:30": 12600000,
	"+04:00": 14400000,
	"+04:30": 16200000,
	"+05:00": 18000000,
	"+05:30": 19800000,
	"+05:45": 20700000,
	"+06:00": 21600000,
	"+06:30": 23400000,
	"+07:00": 25200000,
	"+08:00": 28800000,
	"+08:45": 31500000,
	"+09:00": 32400000,
	"+09:30": 34200000,
	"+10:00": 36000000,
	"+10:30": 37800000,
	"+11:00": 39600000,
	"+12:00": 43200000,
	"+12:45": 45900000,
	"+13:00": 46800000,
	"+14:00": 50400000,
}

func TestZoneByOffset(t *testing.T) {
	for k, v := range TimeZoneOffset {

		t.Log(k, v)
		t.Log(ZoneByOffset(v))
	}
}
