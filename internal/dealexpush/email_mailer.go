package dealexpush

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// EmailMessage 是一封纯文本 + HTML 双版本的邮件。
type EmailMessage struct {
	From      string
	To        string
	Subject   string
	Text      string
	HTML      string
	MessageID string
}

// EmailMailer 抽象邮件发送，便于用 fake SMTP 做单测。
type EmailMailer interface {
	Send(ctx context.Context, msg EmailMessage) (messageID string, err error)
}

// SMTPConfig 是解析后的 SMTP 连接参数（密码只驻留内存，绝不写日志）。
type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

// smtpMailer 用标准库 net/smtp 发信：465 走隐式 TLS，其余端口走 smtp.SendMail（自动 STARTTLS）。
type smtpMailer struct {
	cfg SMTPConfig
}

// NewSMTPMailer 返回可用的发送器；host/user/pass/from 任一为空时返回 nil，表示未配置。
func NewSMTPMailer(cfg SMTPConfig) EmailMailer {
	if strings.TrimSpace(cfg.Host) == "" || cfg.Port <= 0 ||
		strings.TrimSpace(cfg.User) == "" || strings.TrimSpace(cfg.Pass) == "" ||
		strings.TrimSpace(cfg.From) == "" {
		return nil
	}
	return &smtpMailer{cfg: cfg}
}

func (m *smtpMailer) Send(ctx context.Context, msg EmailMessage) (string, error) {
	if m == nil {
		return "", fmt.Errorf("smtp mailer unavailable")
	}
	if strings.TrimSpace(msg.To) == "" {
		return "", fmt.Errorf("email recipient is required")
	}
	if strings.TrimSpace(msg.MessageID) == "" {
		msg.MessageID = newMessageID(m.cfg.From)
	}
	if strings.TrimSpace(msg.From) == "" {
		msg.From = m.cfg.From
	}
	raw := buildMIMEMessage(msg)

	done := make(chan error, 1)
	go func() {
		done <- m.send(raw, msg.From, msg.To)
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-done:
		if err != nil {
			return "", err
		}
		return msg.MessageID, nil
	}
}

func (m *smtpMailer) send(raw []byte, from, to string) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	auth := smtp.PlainAuth("", m.cfg.User, m.cfg.Pass, m.cfg.Host)
	if m.cfg.Port == 465 {
		return m.sendTLS(addr, auth, from, []string{to}, raw)
	}
	return smtp.SendMail(addr, auth, from, []string{to}, raw)
}

func (m *smtpMailer) sendTLS(addr string, auth smtp.Auth, from string, recipients []string, raw []byte) error {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
		ServerName: m.cfg.Host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := io.Copy(writer, strings.NewReader(string(raw))); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// buildMIMEMessage 生成 multipart/alternative 邮件：正文以 base64 传输，避免中文被截断。
func buildMIMEMessage(msg EmailMessage) []byte {
	boundary := "lingce-" + randomHex(12)
	domain := emailDomain(msg.From)
	if domain == "" {
		domain = "lingce.local"
	}
	var builder strings.Builder
	writeHeader := func(key, value string) {
		builder.WriteString(key)
		builder.WriteString(": ")
		builder.WriteString(value)
		builder.WriteString("\r\n")
	}
	writeHeader("From", msg.From)
	writeHeader("To", msg.To)
	writeHeader("Subject", mime.BEncoding.Encode("UTF-8", msg.Subject))
	writeHeader("Date", time.Now().Format(time.RFC1123Z))
	writeHeader("Message-ID", msg.MessageID)
	writeHeader("MIME-Version", "1.0")
	writeHeader("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	builder.WriteString("\r\n")

	builder.WriteString("--" + boundary + "\r\n")
	builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	builder.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	builder.WriteString(base64Lines(msg.Text))
	builder.WriteString("\r\n")

	builder.WriteString("--" + boundary + "\r\n")
	builder.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	builder.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	builder.WriteString(base64Lines(msg.HTML))
	builder.WriteString("\r\n")

	builder.WriteString("--" + boundary + "--\r\n")
	return []byte(builder.String())
}

func base64Lines(value string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	var builder strings.Builder
	for len(encoded) > 76 {
		builder.WriteString(encoded[:76])
		builder.WriteString("\r\n")
		encoded = encoded[76:]
	}
	builder.WriteString(encoded)
	return builder.String()
}

func newMessageID(from string) string {
	domain := emailDomain(from)
	if domain == "" {
		domain = "lingce.local"
	}
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), randomHex(6), domain)
}

func emailDomain(address string) string {
	address = strings.TrimSpace(address)
	if at := strings.LastIndex(address, "@"); at >= 0 && at+1 < len(address) {
		return strings.Trim(address[at+1:], "> ")
	}
	return ""
}

func randomHex(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", size*2)
	}
	return hex.EncodeToString(buf)
}
