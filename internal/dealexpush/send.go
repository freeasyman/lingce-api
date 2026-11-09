package dealexpush

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/pkg/sms"
)

// TemplateSender 抽象模板短信发送：返回阿里云 BizId；便于单元测试注入 fake。
type TemplateSender interface {
	SendTemplate(phone, signName, templateCode string, params map[string]string) (bizID string, err error)
}

// aliyunSender 适配 pkg/sms 阿里云客户端。
type aliyunSender struct {
	client *sms.AliyunClient
}

func (a aliyunSender) SendTemplate(phone, signName, templateCode string, params map[string]string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("aliyun sms client unavailable")
	}
	result, err := a.client.SendTemplate(phone, signName, templateCode, params)
	if err != nil {
		return "", err
	}
	return result.BizID, nil
}

// SetSender 设置短信发送器；未设置时不会真实发送（默认 nil）。
func (s *Service) SetSender(sender TemplateSender) {
	if s != nil {
		s.sender = sender
	}
}

// whitelisted 判断手机号是否在白名单内（只发给测试号）。
func whitelisted(whitelist []string, phone string) bool {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return false
	}
	for _, item := range whitelist {
		if strings.TrimSpace(item) == phone {
			return true
		}
	}
	return false
}

// decideSend 是否真实发送：enabled 且非干跑 且 事件未发送 且 收件人在白名单。
func decideSend(cfg config.DealPushConfig, status, phone string) bool {
	if !cfg.Enabled {
		return false
	}
	if cfg.IsDryRun() {
		return false
	}
	if status == "sent" {
		return false
	}
	return whitelisted(cfg.Whitelist, phone)
}

// EmployeePhone 返回员工手机号（employees.phone）。
func (s *Service) EmployeePhone(ctx context.Context, employeeID int64) (string, error) {
	var phone string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(phone, '') FROM employees WHERE id = $1
	`, employeeID).Scan(&phone)
	if err != nil {
		return "", fmt.Errorf("load employee phone: %w", err)
	}
	return strings.TrimSpace(phone), nil
}

// pushEventRow 是发送所需的单条事件信息。
type pushEventRow struct {
	ID                   int64
	TenantID             int64
	RecipientEmployeeID  int64
	TemplateCode         string
	Status               string
	ProviderBizID        string
	Payload              json.RawMessage
}

// loadEvent 读取单条推送事件。
func (s *Service) loadEvent(ctx context.Context, eventID int64) (*pushEventRow, error) {
	var row pushEventRow
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, recipient_employee_id, template_code, status, provider_biz_id, payload
		FROM deal_push_events WHERE id = $1
	`, eventID).Scan(&row.ID, &row.TenantID, &row.RecipientEmployeeID, &row.TemplateCode, &row.Status, &row.ProviderBizID, &row.Payload)
	if err != nil {
		return nil, fmt.Errorf("load push event: %w", err)
	}
	return &row, nil
}

// templateParams 从事件 payload 提取模板变量（name/code/link）。
func templateParams(payload []byte) map[string]string {
	out := map[string]string{}
	var obj map[string]any
	if payload == nil || json.Unmarshal(payload, &obj) != nil {
		return out
	}
	for _, key := range []string{"name", "code", "link"} {
		if value, ok := obj[key].(string); ok && strings.TrimSpace(value) != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}

// tryAutoSend 在事件创建后按开关决定是否真实发送；否则保持 dry_run。
// 只发给白名单内号码；失败只记日志，不阻断分析。
func (s *Service) tryAutoSend(ctx context.Context, eventID, recipientEmployeeID int64) {
	if !s.cfg.Enabled || s.cfg.IsDryRun() {
		return
	}
	phone, err := s.EmployeePhone(ctx, recipientEmployeeID)
	if err != nil || !whitelisted(s.cfg.Whitelist, phone) {
		s.log.Info("DRY RUN push: not sent (not in whitelist or no phone)", "event_id", eventID)
		return
	}
	if _, _, err := s.sendEvent(ctx, eventID); err != nil {
		s.log.Warn("push send failed", "event_id", eventID, "error", err)
	}
}

// sendOnce 执行一次真实发送（白名单校验 + 发送器调用），不写库；便于单测注入 fake。
func (s *Service) sendOnce(phone, sign, template string, params map[string]string) (status, bizID string, err error) {
	if !whitelisted(s.cfg.Whitelist, phone) {
		return "failed", "", fmt.Errorf("recipient phone not in whitelist")
	}
	if s.sender == nil {
		return "failed", "", fmt.Errorf("sms sender unavailable")
	}
	bizID, err = s.sender.SendTemplate(phone, sign, template, params)
	if err != nil {
		return "failed", "", err
	}
	return "sent", bizID, nil
}

// sendEvent 对单条事件执行发送并写回结果。幂等：status=sent 不重发；
// 发送方错误写 failed（可经管理接口显式重试）。
func (s *Service) sendEvent(ctx context.Context, eventID int64) (status, bizID string, err error) {
	row, err := s.loadEvent(ctx, eventID)
	if err != nil {
		return "", "", err
	}
	if row.Status == "sent" {
		return "sent", row.ProviderBizID, nil
	}
	phone, err := s.EmployeePhone(ctx, row.RecipientEmployeeID)
	if err != nil {
		return "", "", err
	}
	if phone == "" {
		return "", "", fmt.Errorf("recipient has no phone")
	}
	status, bizID, err = s.sendOnce(phone, s.cfg.SMSSignName, row.TemplateCode, templateParams(row.Payload))
	if err != nil {
		_, _ = s.pool.Exec(ctx, `
			UPDATE deal_push_events SET status = 'failed', error = $2, sent_at = NOW()
			WHERE id = $1 AND status <> 'sent'
		`, eventID, truncateString(err.Error(), 500))
		return "failed", "", err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE deal_push_events
		SET status = 'sent', error = '', sent_at = NOW(), provider_biz_id = $2
		WHERE id = $1 AND status <> 'sent'
	`, eventID, bizID); err != nil {
		return "", "", fmt.Errorf("mark push event sent: %w", err)
	}
	s.log.Info("push sms sent", "event_id", eventID, "biz_id", bizID, "phone_masked", maskPhone(phone))
	return "sent", bizID, nil
}

func truncateString(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func maskPhone(phone string) string {
	runes := []rune(phone)
	if len(runes) < 7 {
		return "***"
	}
	return string(runes[:3]) + "****" + string(runes[len(runes)-4:])
}
