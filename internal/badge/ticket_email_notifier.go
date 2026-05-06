package badge

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/smtp"
	"strconv"
	"strings"
)

type TicketEmailNotifier struct {
	host     string
	port     int
	username string
	password string
	from     string
	to       []string
}

func MissingTicketEmailNotifierFields(host string, port int, username, password, from, to string) []string {
	var missing []string
	if strings.TrimSpace(host) == "" {
		missing = append(missing, "TICKET_NOTIFY_SMTP_HOST")
	}
	if port <= 0 {
		missing = append(missing, "TICKET_NOTIFY_SMTP_PORT")
	}
	if strings.TrimSpace(username) == "" {
		missing = append(missing, "TICKET_NOTIFY_SMTP_USER")
	}
	if strings.TrimSpace(password) == "" {
		missing = append(missing, "TICKET_NOTIFY_SMTP_PASS")
	}
	if strings.TrimSpace(from) == "" {
		missing = append(missing, "TICKET_NOTIFY_FROM")
	}
	if strings.TrimSpace(to) == "" {
		missing = append(missing, "TICKET_NOTIFY_TO")
	}
	return missing
}

func NewTicketEmailNotifier(host string, port int, username, password, from, to string) *TicketEmailNotifier {
	host = strings.TrimSpace(host)
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if len(MissingTicketEmailNotifierFields(host, port, username, password, from, to)) > 0 {
		return nil
	}
	recipients := parseRecipients(to)
	if len(recipients) == 0 {
		return nil
	}
	return &TicketEmailNotifier{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		to:       recipients,
	}
}

func (n *TicketEmailNotifier) SendTicketCreated(ctx context.Context, t *BadgeTicket) error {
	if n == nil || t == nil {
		return nil
	}
	addr := fmt.Sprintf("%s:%d", n.host, n.port)
	auth := smtp.PlainAuth("", n.username, n.password, n.host)

	device := ""
	if t.DeviceNo != nil {
		device = strings.TrimSpace(*t.DeviceNo)
	}
	if device == "" && t.DeviceID != nil {
		device = strconv.FormatInt(*t.DeviceID, 10)
	}
	if device == "" {
		device = "未知"
	}

	submitterName := "未知"
	if t.SubmitterName != nil && strings.TrimSpace(*t.SubmitterName) != "" {
		submitterName = strings.TrimSpace(*t.SubmitterName)
	}

	tenantName := "未知"
	if t.TenantName != nil && strings.TrimSpace(*t.TenantName) != "" {
		tenantName = strings.TrimSpace(*t.TenantName)
	}

	subject := fmt.Sprintf(
		"[%s] 工牌工单：%s（%s）",
		tenantName,
		compactForSubject(t.Description, 28),
		t.TicketNo,
	)
	body := fmt.Sprintf(
		"有新的工牌工单提交。\r\n\r\n工单号：%s\r\n类型：%s\r\n状态：%s\r\n设备：%s\r\n标题：%s\r\n描述：%s\r\n提交人：%s\r\n医疗机构：%s\r\n提交时间：%s\r\n",
		t.TicketNo,
		ticketTypeLabelCN(t.Type),
		ticketStatusLabelCN(t.Status),
		device,
		t.Title,
		t.Description,
		submitterName,
		tenantName,
		t.SubmittedAt.Format("2006-01-02 15:04:05"),
	)
	msg := []byte("To: " + strings.Join(n.to, ", ") + "\r\n" +
		"From: " + n.from + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		body)

	done := make(chan error, 1)
	go func() {
		if n.port == 465 {
			done <- n.sendMailTLS(addr, auth, []byte(msg))
			return
		}
		done <- smtp.SendMail(addr, auth, n.from, n.to, msg)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func (n *TicketEmailNotifier) sendMailTLS(addr string, auth smtp.Auth, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: n.host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, n.host)
	if err != nil {
		return err
	}
	defer client.Quit()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(n.from); err != nil {
		return err
	}
	for _, recipient := range n.to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, strings.NewReader(string(msg))); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func parseRecipients(raw string) []string {
	raw = strings.ReplaceAll(raw, ";", ",")
	parts := strings.Split(raw, ",")
	recipients := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		email := strings.TrimSpace(part)
		if email == "" {
			continue
		}
		key := strings.ToLower(email)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		recipients = append(recipients, email)
	}
	return recipients
}

func ticketTypeLabelCN(code string) string {
	switch strings.TrimSpace(code) {
	case "maintenance":
		return "设备维修"
	case "replacement":
		return "设备更换"
	case "reclaim":
		return "设备回收"
	case "exception":
		return "异常处理"
	case "device_lost":
		return "设备遗失"
	case "recording_error":
		return "录音异常"
	default:
		return "其他"
	}
}

func ticketStatusLabelCN(code string) string {
	switch strings.TrimSpace(code) {
	case "pending":
		return "待审批"
	case "approved":
		return "待执行"
	case "rejected":
		return "已驳回"
	case "executing":
		return "执行中"
	case "completed":
		return "已完成"
	case "cancelled":
		return "已取消"
	default:
		return "未知"
	}
}

func compactForSubject(raw string, maxLen int) string {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "未填写问题描述"
	}
	runes := []rune(s)
	if maxLen > 0 && len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}
