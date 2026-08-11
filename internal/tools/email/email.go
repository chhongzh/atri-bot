package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"

	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/jordan-wright/email"
)

type config struct {
	SmtpHost    string `json:"smtpHost"`
	SmtpPort    int    `json:"smtpPort"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	FromAddress string `json:"fromAddress"`
}

type input struct {
	Subject string   `json:"subject" jsonschema:"required" jsonschema_description:"邮件的主题"`
	Html    string   `json:"html" jsonschema:"required" jsonschema_description:"邮件的 HTML 正文"`
	To      []string `json:"to" jsonschema:"required" jsonschema_description:"接收者的邮箱地址"`
	Cc      []string `json:"cc" jsonschema_description:"抄送者的邮箱地址"`
}

type result struct {
	Delivered bool `json:"delivered"`
}

func mailTool(ctx context.Context, cfg *config, input *input) (*result, error) {
	if err := validateInput(input); err != nil {
		return nil, err
	}
	host, port, auth, err := validateConfig(cfg)
	if err != nil {
		return nil, err
	}

	e := email.NewEmail()
	e.From = strings.TrimSpace(cfg.FromAddress)
	e.To = trimAddresses(input.To)
	e.Cc = trimAddresses(input.Cc)
	e.Subject = input.Subject
	e.HTML = []byte(input.Html)

	address := net.JoinHostPort(host, strconv.Itoa(port))
	if port == 465 {
		err = e.SendWithTLS(address, auth, &tls.Config{ServerName: host})
	} else {
		// smtp.SendMail negotiates STARTTLS when the server advertises it.
		err = e.Send(address, auth)
	}
	if err != nil {
		return nil, fmt.Errorf("send email via %s: %w", address, err)
	}

	return &result{Delivered: true}, nil
}

func validateInput(input *input) error {
	if input == nil {
		return errors.New("邮件内容不能为空")
	}
	if len(input.Html) == 0 {
		return errors.New("正文不能为空")
	}
	if len(trimAddresses(input.To)) == 0 {
		return errors.New("至少要有一个人收件")
	}
	for _, address := range append(trimAddresses(input.To), trimAddresses(input.Cc)...) {
		if _, err := mail.ParseAddress(address); err != nil {
			return fmt.Errorf("无效的收件人地址 %q: %w", address, err)
		}
	}
	return nil
}

func validateConfig(cfg *config) (string, int, smtp.Auth, error) {
	host := strings.TrimSpace(cfg.SmtpHost)
	if host == "" {
		return "", 0, nil, errors.New("smtpHost 不能为空")
	}
	if cfg.SmtpPort < 1 || cfg.SmtpPort > 65535 {
		return "", 0, nil, fmt.Errorf("smtpPort 必须介于 1 和 65535 之间，当前为 %d", cfg.SmtpPort)
	}
	from := strings.TrimSpace(cfg.FromAddress)
	if _, err := mail.ParseAddress(from); err != nil {
		return "", 0, nil, fmt.Errorf("无效的 fromAddress: %w", err)
	}
	username := strings.TrimSpace(cfg.Username)
	password := strings.TrimSpace(cfg.Password)
	if (username == "") != (password == "") {
		return "", 0, nil, errors.New("username 和 password 必须同时配置")
	}
	if username == "" {
		return host, cfg.SmtpPort, nil, nil
	}
	return host, cfg.SmtpPort, smtp.PlainAuth("", username, password, host), nil
}

func trimAddresses(addresses []string) []string {
	trimmed := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address = strings.TrimSpace(address); address != "" {
			trimmed = append(trimmed, address)
		}
	}
	return trimmed
}

func Register(manager *toolmanager.Manager) error {
	return toolmanager.Register(manager, "send_mail", "发送邮件", config{},
		func(ctx context.Context, _ *toolmanager.RunningState, cfg *config, input *input) (*result, error) {
			return mailTool(ctx, cfg, input)
		})
}
