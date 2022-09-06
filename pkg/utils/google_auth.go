package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"fmt"
	"strings"
	"time"
)

type GoogleAuth struct {
}

func (g *GoogleAuth) GetSecret() string {
	randomStr := g.randStr(16)
	return strings.ToUpper(randomStr)
}

func (g *GoogleAuth) GetQRBarcode(user string, secret string) string {
	return "otpauth://totp/" + user + "?secret=" + secret
}

func (g *GoogleAuth) randStr(strSize int) string {
	dictionary := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var bytes = make([]byte, strSize)
	_, _ = rand.Read(bytes)
	for k, v := range bytes {
		bytes[k] = dictionary[v%byte(len(dictionary))]
	}
	return string(bytes)
}

// VerifyCode 为了考虑时间误差，判断前当前时间及前后30秒时间
func (g *GoogleAuth) VerifyCode(secret string, code int32) bool {
	// 当前google值
	if g.getCode(secret, 0) == code {
		return true
	}

	// 前30秒google值
	if g.getCode(secret, -30) == code {
		return true
	}
	// 后30秒google值
	if g.getCode(secret, 30) == code {
		return true
	}
	return false
}

func (g *GoogleAuth) getCode(secret string, offset int64) int32 {
	key, err := base32.StdEncoding.DecodeString(secret)
	if err != nil {
		fmt.Println(err)
		return 0
	}
	epochSeconds := time.Now().Unix() + offset
	return int32(g.oneTimePassword(key, g.toBytes(epochSeconds/30)))
}

func (g *GoogleAuth) toBytes(value int64) []byte {
	var result []byte
	mask := int64(0xFF)
	shifts := [8]uint16{56, 48, 40, 32, 24, 16, 8, 0}
	for _, shift := range shifts {
		result = append(result, byte((value>>shift)&mask))
	}
	return result
}

func (g *GoogleAuth) toUint32(bytes []byte) uint32 {
	return (uint32(bytes[0]) << 24) + (uint32(bytes[1]) << 16) +
		(uint32(bytes[2]) << 8) + uint32(bytes[3])
}

func (g *GoogleAuth) oneTimePassword(key []byte, value []byte) uint32 {
	// sign the value using HMAC-SHA1
	hmacSha1 := hmac.New(sha1.New, key)
	hmacSha1.Write(value)
	hash := hmacSha1.Sum(nil)

	// We're going to use a subset of the generated hash.
	// Using the last nibble (half-byte) to choose the index to start from.
	// This number is always appropriate as it's maximum decimal 15, the hash will
	// have the maximum index 19 (20 bytes of SHA1) and we need 4 bytes.
	offset := hash[len(hash)-1] & 0x0F

	// get a 32-bit (4-byte) chunk from the hash starting at offset
	hashParts := hash[offset : offset+4]

	// ignore the most significant bit as per RFC 4226
	hashParts[0] = hashParts[0] & 0x7F
	number := g.toUint32(hashParts)

	// size to 6 digits
	// one million is the first number with 7 digits so the remainder
	// of the division will always return < 7 digits
	pwd := number % 1000000
	return pwd
}
