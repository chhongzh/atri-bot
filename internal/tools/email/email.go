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

	"github.com/chhongzh/atri-bot/internal/security"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/jordan-wright/email"
)

const (
	toolName        = "send_mail"
	toolDescription = `Send an email over SMTP using the configured sender address. Supports an HTML body, one or more primary recipients, and optional CC recipients.`
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

func mailTool(ctx context.Context, cfg *config, input *input, allowPrivateIP bool) (*result, error) {
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
		err = sendWithTLS(ctx, e, address, host, auth, allowPrivateIP)
	} else {
		err = sendWithStartTLS(ctx, e, address, host, auth, allowPrivateIP)
	}
	if err != nil {
		return nil, fmt.Errorf("send email via %s: %w", address, err)
	}

	return &result{Delivered: true}, nil
}

func sendWithTLS(ctx context.Context, e *email.Email, address, host string, auth smtp.Auth, allowPrivateIP bool) error {
	connection, err := dialSMTP(ctx, address, allowPrivateIP)
	if err != nil {
		return err
	}
	return sendSMTP(e, tls.Client(connection, &tls.Config{ServerName: host}), host, auth, nil)
}

func sendWithStartTLS(ctx context.Context, e *email.Email, address, host string, auth smtp.Auth, allowPrivateIP bool) error {
	connection, err := dialSMTP(ctx, address, allowPrivateIP)
	if err != nil {
		return err
	}
	return sendSMTP(e, connection, host, auth, &tls.Config{ServerName: host})
}

func dialSMTP(ctx context.Context, address string, allowPrivateIP bool) (net.Conn, error) {
	return security.NewSafeDialer(&net.Dialer{}, allowPrivateIP).DialContext(ctx, "tcp", address)
}

func sendSMTP(e *email.Email, connection net.Conn, host string, auth smtp.Auth, startTLSConfig *tls.Config) error {
	defer func() { _ = connection.Close() }()
	client, err := smtp.NewClient(connection, host)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if err = client.Hello("localhost"); err != nil {
		return err
	}
	if startTLSConfig != nil {
		if supported, _ := client.Extension("STARTTLS"); supported {
			if err = client.StartTLS(startTLSConfig); err != nil {
				return err
			}
		}
	}
	if auth != nil {
		if supported, _ := client.Extension("AUTH"); supported {
			if err = client.Auth(auth); err != nil {
				return err
			}
		}
	}
	from, err := mail.ParseAddress(e.From)
	if err != nil {
		return err
	}
	recipients := append(append([]string{}, e.To...), e.Cc...)
	if len(recipients) == 0 {
		return errors.New("at least one recipient is required")
	}
	if err = client.Mail(from.Address); err != nil {
		return err
	}
	for _, recipient := range recipients {
		parsed, parseErr := mail.ParseAddress(recipient)
		if parseErr != nil {
			return parseErr
		}
		if err = client.Rcpt(parsed.Address); err != nil {
			return err
		}
	}
	raw, err := e.Bytes()
	if err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(raw); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
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

func BindedRegister(allowPrivateIP bool) func(manager *toolmanager.Manager) error {
	return func(manager *toolmanager.Manager) error {
		return toolmanager.Register(manager, toolName, toolDescription, config{},
			func(ctx context.Context, _ *toolmanager.RunningState, cfg *config, input *input) (*result, error) {
				return mailTool(ctx, cfg, input, allowPrivateIP)
			})
	}
}
