package utl

import (
	"testing"
)

// 测试发送邮件的功能
func TestEmail_Send(t *testing.T) {
	// 创建一个新的Email对象 163邮箱服务器
	email := NewEmail("smtp.163.com", 25, "wskfjtheqianb@163.com", "password123")

	// 用于测试的参数
	subject := "测试邮件主题"
	body := "这是邮件内容"
	validAddress := "recipient@example.com"

	// -------------------------------
	// 测试用例1：正常情况（happy path）
	// -------------------------------
	err := email.Send(subject, body, validAddress)
	if err != nil {
		t.Errorf("发送邮件失败：%v", err)
	}

	// -------------------------------
	// 测试用例2：地址为空（边界情况）
	// -------------------------------
	err = email.Send(subject, body) // 地址为空
	if err == nil {
		t.Error("发送邮件应失败，但没有返回错误")
	}

	// -------------------------------
	// 测试用例3：邮件内容为空（边界情况）
	// -------------------------------
	err = email.Send(subject, "", validAddress) // 内容为空
	if err == nil {
		t.Error("发送邮件应失败，但没有返回错误")
	}

	// -------------------------------
	// 测试用例4：主题为空（边界情况）
	// -------------------------------
	err = email.Send("", body, validAddress) // 主题为空
	if err != nil {
		t.Errorf("发送邮件失败：%v", err)
	}

	// -------------------------------
	// 测试用例5：未连接SMTP服务器（边界情况）
	// -------------------------------
	invalidEmail := NewEmail("invalid.smtp.com", 0, "test@example.com", "password123")
	err = invalidEmail.Send(subject, body, validAddress) // 无法连接的SMTP服务器
	if err == nil {
		t.Error("发送邮件应失败，但没有返回错误")
	}
}
