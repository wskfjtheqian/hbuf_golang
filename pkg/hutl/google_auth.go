package hutl

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"fmt"
	"strings"
	"time"
)

// NewGoogleAuth 创建谷歌身份验证器
func NewGoogleAuth() *GoogleAuth {
	return &GoogleAuth{}
}

// GoogleAuth 谷歌身份验证器
type GoogleAuth struct {
}

// GetSecret 生成随机秘钥
func (g *GoogleAuth) GetSecret() string {
	randomStr := g.randStr(16)
	return strings.ToUpper(randomStr)
}

// GetQRBarcode 生成谷歌身份验证器的二维码
func (g *GoogleAuth) GetQRBarcode(user string, secret string) string {
	return "otpauth://totp/" + user + "?secret=" + secret
}

// randStr 生成随机字符串
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

// getCode 获取谷歌身份验证器的验证码
func (g *GoogleAuth) getCode(secret string, offset int64) int32 {
	key, err := base32.StdEncoding.DecodeString(secret)
	if err != nil {
		fmt.Println(err)
		return 0
	}
	epochSeconds := time.Now().Unix() + offset
	return int32(g.oneTimePassword(key, g.toBytes(epochSeconds/30)))
}

// toBytes 将时间戳转换为字节数组
func (g *GoogleAuth) toBytes(value int64) []byte {
	var result []byte
	mask := int64(0xFF)
	shifts := [8]uint16{56, 48, 40, 32, 24, 16, 8, 0}
	for _, shift := range shifts {
		result = append(result, byte((value>>shift)&mask))
	}
	return result
}

// toUint32 将字节数组转换为32位无符号整数
func (g *GoogleAuth) toUint32(bytes []byte) uint32 {
	return (uint32(bytes[0]) << 24) + (uint32(bytes[1]) << 16) +
		(uint32(bytes[2]) << 8) + uint32(bytes[3])
}

// oneTimePassword 生成谷歌身份验证器的一次性密码
func (g *GoogleAuth) oneTimePassword(key []byte, value []byte) uint32 {
	// 计算HMAC-SHA1
	hmacSha1 := hmac.New(sha1.New, key)
	hmacSha1.Write(value)
	hash := hmacSha1.Sum(nil)

	// 取最后4个字节作为OTP
	offset := hash[len(hash)-1] & 0x0F

	// 取倒数4个字节作为OTP
	hashParts := hash[offset : offset+4]

	// 取倒数4个字节的最后一位作为OTP
	hashParts[0] = hashParts[0] & 0x7F
	number := g.toUint32(hashParts)

	// 取倒数4个字节的倒数3位作为OTP
	pwd := number % 1000000
	return pwd
}
