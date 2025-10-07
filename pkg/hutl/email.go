package hutl

import (
	"crypto/tls"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"net/smtp"
	"strconv"
	"strings"
)

//发送邮件

// NewEmail 		新建一个Email对象
// Send 			发送邮件
// smtpServer 	smtp服务器地址
// smtpPort 		smtp服务器端口
// account 		邮箱账号
// password 		邮箱密码
func NewEmail(smtpServer string, smtpPort int, account string, password string) *Email {
	return &Email{
		smtpServer: smtpServer,
		account:    account,
		password:   password,
		smtpPort:   smtpPort,
	}
}

// Email 邮件对象
type Email struct {
	smtpServer string // smtp服务器地址
	smtpPort   int    // smtp服务器地址
	account    string // 邮箱账号
	password   string // 邮箱密码
}

// Send 发送邮件
// subject 邮件主题
// body 邮件内容
// address 收件人地址
// 返回错误
func (e Email) Send(subject, body string, address ...string) error {
	if len(address) == 0 || len(body) == 0 {
		return herror.NewError("address or body is empty")
	}

	// 通常身份应该是空字符串，填充用户名.
	auth := smtp.PlainAuth("", e.account, e.password, e.smtpServer)
	contentType := "Content-Type: text/html; charset=UTF-8"

	data := strings.Builder{}
	data.WriteString("To:")
	data.WriteString(address[0])
	data.WriteString("\r\n")

	data.WriteString("From:")
	data.WriteString(e.account)
	data.WriteString("<")
	data.WriteString(e.account)
	data.WriteString(">\r\n")

	if 0 == len(subject) {
		data.WriteString("Subject:")
		data.WriteString(subject)
		data.WriteString("\r\n")
	}

	data.WriteString(contentType)
	data.WriteString("\r\n\r\n")
	data.WriteString(body)

	conn, err := tls.Dial("tcp", e.smtpServer+":"+strconv.Itoa(e.smtpPort), &tls.Config{
		ServerName: e.smtpServer,
	})
	if err != nil {
		return herror.Wrap(err)
	}

	client, err := smtp.NewClient(conn, e.smtpServer)
	if err != nil {
		return herror.Wrap(err)
	}
	if ok, _ := client.Extension("AUTH"); ok {
		if err = client.Auth(auth); err != nil {
			return herror.Wrap(err)
		}
	}
	// Set the sender and recipient first
	if err := client.Mail(e.account); err != nil {
		return herror.Wrap(err)
	}
	for _, addr := range address {
		if err := client.Rcpt(addr); err != nil {
			return herror.Wrap(err)
		}
	}
	// Send the email body.
	wc, err := client.Data()
	if err != nil {
		return herror.Wrap(err)
	}

	_, err = wc.Write([]byte(data.String()))
	if err != nil {
		return herror.Wrap(err)
	}
	err = wc.Close()
	if err != nil {
		return herror.Wrap(err)
	}

	err = client.Quit()
	if err != nil {
		return herror.Wrap(err)
	}

	defer client.Close()
	return nil
}
