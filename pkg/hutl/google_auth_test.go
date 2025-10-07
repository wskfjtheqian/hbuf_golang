package hutl

import (
	"testing"
)

// 测试GoogleAuth结构体
func TestGoogleAuth(t *testing.T) {
	g := NewGoogleAuth()

	// 测试GetSecret函数，检查生成的秘钥长度是否正确
	secret := g.GetSecret()
	if len(secret) != 16 {
		t.Errorf("GetSecret() = %s; 期望长度为 16", secret)
	}

	// 测试GetQRBarcode函数，检查二维码生成的格式
	user := "exampleUser"
	qr := g.GetQRBarcode(user, secret)
	expected := "otpauth://totp/" + user + "?secret=" + secret
	if qr != expected {
		t.Errorf("GetQRBarcode() = %s; 期望为 %s", qr, expected)
	}

	// 测试VerifyCode函数
	code := g.getCode(secret, 0)     // 获取当前时间的验证码
	if !g.VerifyCode(secret, code) { // 检查当前验证码是否正确
		t.Errorf("VerifyCode() 对于当前验证码 %d 失败", code)
	}

	// 测试边界情况：前后30秒验证码
	if !g.VerifyCode(secret, g.getCode(secret, -30)) { // 前30秒的验证码
		t.Errorf("VerifyCode() 对于前30秒验证码失败")
	}
	if !g.VerifyCode(secret, g.getCode(secret, 30)) { // 后30秒的验证码
		t.Errorf("VerifyCode() 对于后30秒验证码失败")
	}

	// 测试无效的验证码
	if g.VerifyCode(secret, 123456) { // 这里的123456是一个明显无效的验证码
		t.Errorf("VerifyCode() 对于无效验证码失败")
	}
}
