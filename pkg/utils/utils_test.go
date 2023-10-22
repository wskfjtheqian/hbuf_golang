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
