package alert

import (
	"context"
	"fmt"
	"strconv"

	"gopkg.in/gomail.v2"
	"k8s.io/klog/v2"

	"github.com/raids-lab/crater/dao/model"
	config "github.com/raids-lab/crater/pkg/config"
)

type SMTPAlerter struct {
	host     string
	port     int
	username string
	password string
	from     string
	fromName string // 添加发件人昵称字段
}

func newSMTPAlerter() (alertHandlerInterface, error) {
	smtpConfig := config.GetConfig()
	smtpHost := smtpConfig.SMTP.Host
	smtpPort := smtpConfig.SMTP.Port

	// 将端口字符串转换为整数
	port, err := strconv.Atoi(smtpPort)
	if err != nil {
		return nil, fmt.Errorf("invalid SMTP port: %w", err)
	}

	// 使用固定昵称"Crater System"，也可以从配置中获取
	fromName := "Crater System"

	return &SMTPAlerter{
		host:     smtpHost,
		port:     port,
		username: smtpConfig.SMTP.User,
		password: smtpConfig.SMTP.Password,
		from:     smtpConfig.SMTP.Notify,
		fromName: fromName,
	}, nil
}

func (sa *SMTPAlerter) SendMessageTo(_ context.Context, receiver *model.UserAttribute, subject, body string) error {
	if receiver.Email == nil {
		klog.Warningf("%s does not have an email address", receiver.Name)
		return nil
	}

	m := gomail.NewMessage()
	// 使用SetAddressHeader方法设置发件人，让gomail处理编码
	m.SetAddressHeader("From", sa.from, sa.fromName)
	// 使用SetAddressHeader方法设置收件人，让gomail处理编码
	m.SetAddressHeader("To", *receiver.Email, receiver.Nickname)
	m.SetHeader("Subject", fmt.Sprintf("[Crater] %s", subject))
	m.SetBody("text/html", body)

	d := gomail.NewDialer(sa.host, sa.port, sa.username, sa.password)
	// 禁用SSL/TLS，如果服务器不支持
	d.SSL = false

	if err := d.DialAndSend(m); err != nil {
		klog.Errorf("Failed to send email to %s: %v", *receiver.Email, err)
		return err
	}

	klog.Infof("Sent email to %s", *receiver.Email)
	return nil
}
