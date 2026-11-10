package dealexpush

import (
	"context"
	"strings"
	"time"
)

// expoPushEvent 是一次事件推送所需的上下文。
type expoPushEvent struct {
	EventID             int64
	TenantID            int64
	RecordingID         int64
	RecipientEmployeeID int64
	RecipientRole       string
	CustomerName        string // 已脱敏的称呼（如“杜女士”），绝不使用全名
}

// sendExpoForEvent 查询收件人的有效设备 token，调用 Expo Push 并记录结果。
// 无 token 静默跳过；网络/Expo 失败只记录，不向业务返回错误。
func (s *Service) sendExpoForEvent(ctx context.Context, event expoPushEvent) {
	if s == nil || s.devices == nil || s.expo == nil {
		return
	}
	if event.EventID <= 0 || event.TenantID <= 0 || event.RecipientEmployeeID <= 0 {
		return
	}

	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	devices, err := s.devices.ActiveTokens(sendCtx, event.TenantID, event.RecipientEmployeeID)
	if err != nil {
		s.log.Warn("expo push: load active tokens failed",
			"event_id", event.EventID, "employee_id", event.RecipientEmployeeID, "error", err)
		s.recordExpoEventResult(sendCtx, event.EventID, "failed", "load active tokens: "+err.Error())
		return
	}
	if len(devices) == 0 {
		s.log.Info("expo push: no active device, skip",
			"event_id", event.EventID, "employee_id", event.RecipientEmployeeID)
		s.recordExpoEventResult(sendCtx, event.EventID, "skipped", "no_active_device")
		return
	}

	title, body := expoPushCopy(event.RecipientRole, event.CustomerName)
	data := map[string]any{
		"type":         expoPushType(event.RecipientRole),
		"recording_id": event.RecordingID,
		"employee_id":  event.RecipientEmployeeID,
	}
	messages := make([]ExpoMessage, 0, len(devices))
	for _, device := range devices {
		token := strings.TrimSpace(device.Token)
		if token == "" {
			continue
		}
		messages = append(messages, ExpoMessage{
			To:    token,
			Title: title,
			Body:  body,
			Data:  data,
			Sound: "default",
		})
	}
	if len(messages) == 0 {
		s.recordExpoEventResult(sendCtx, event.EventID, "skipped", "no_active_device")
		return
	}

	tickets, sendErr := s.expo.Send(sendCtx, messages)
	if sendErr != nil {
		s.log.Warn("expo push: send failed",
			"event_id", event.EventID, "employee_id", event.RecipientEmployeeID, "error", sendErr)
		for _, message := range messages {
			s.markDeviceFailed(sendCtx, event, message.To, sendErr.Error())
		}
		s.recordExpoEventResult(sendCtx, event.EventID, "failed", sendErr.Error())
		return
	}

	now := time.Now()
	successCount := 0
	errors := make([]string, 0)
	for index, message := range messages {
		if index >= len(tickets) {
			s.markDeviceFailed(sendCtx, event, message.To, "missing expo ticket")
			errors = append(errors, "missing expo ticket")
			continue
		}
		ticket := tickets[index]
		if strings.EqualFold(ticket.Status, "ok") {
			successCount++
			if err := s.devices.MarkSent(sendCtx, event.TenantID, event.RecipientEmployeeID, message.To, now); err != nil {
				s.log.Warn("expo push: mark device sent failed", "error", err)
			}
			continue
		}
		reason := strings.TrimSpace(ticket.Message)
		if ticket.ErrorCode != "" {
			reason = strings.TrimSpace(ticket.ErrorCode + ": " + reason)
		}
		if strings.EqualFold(ticket.ErrorCode, "DeviceNotRegistered") {
			if err := s.devices.MarkInvalid(sendCtx, event.TenantID, event.RecipientEmployeeID, message.To, reason); err != nil {
				s.log.Warn("expo push: invalidate device failed", "error", err)
			}
		} else {
			s.markDeviceFailed(sendCtx, event, message.To, reason)
		}
		errors = append(errors, reason)
	}

	status := "failed"
	switch {
	case successCount == len(messages):
		status = "sent"
	case successCount > 0:
		status = "partial"
	}
	s.recordExpoEventResult(sendCtx, event.EventID, status, summarizeExpoErrors(errors))
	s.log.Info("expo push finished",
		"event_id", event.EventID,
		"employee_id", event.RecipientEmployeeID,
		"type", expoPushType(event.RecipientRole),
		"devices", len(messages),
		"success", successCount,
		"status", status)
}

func (s *Service) markDeviceFailed(ctx context.Context, event expoPushEvent, token, reason string) {
	if err := s.devices.MarkFailed(ctx, event.TenantID, event.RecipientEmployeeID, token, reason); err != nil {
		s.log.Warn("expo push: mark device failed", "error", err)
	}
}

// recordExpoEventResult 把发送结果写回事件（expo_* 字段与短信结果分开）。
// 即使发送阶段超时也要落库，因此不继承取消信号。
func (s *Service) recordExpoEventResult(ctx context.Context, eventID int64, status, errMsg string) {
	if s == nil || s.pool == nil || eventID <= 0 {
		return
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, err := s.pool.Exec(recordCtx, `
		UPDATE deal_push_events
		SET expo_status = $2,
		    expo_error = $3,
		    expo_sent_at = CASE WHEN $2 IN ('sent', 'partial') THEN NOW() ELSE expo_sent_at END
		WHERE id = $1
	`, eventID, status, truncateString(errMsg, 500))
	if err != nil {
		s.log.Warn("expo push: record event result failed", "event_id", eventID, "error", err)
	}
}

func summarizeExpoErrors(items []string) string {
	if len(items) == 0 {
		return ""
	}
	limit := len(items)
	if limit > 3 {
		limit = 3
	}
	return truncateString(strings.Join(items[:limit], "; "), 500)
}

// expoPushType 决定 App 深链类型：咨询师事件 → encounter；负责人/介入 → intervention。
func expoPushType(recipientRole string) string {
	switch strings.ToLower(strings.TrimSpace(recipientRole)) {
	case "manager", "intervention":
		return "intervention"
	default:
		return "encounter"
	}
}

// expoPushCopy 只使用客户称呼与一句话，不放金额、病情或全名。
func expoPushCopy(recipientRole, salutation string) (title, body string) {
	name := strings.TrimSpace(salutation)
	if name == "" {
		name = "客户"
	}
	if expoPushType(recipientRole) == "intervention" {
		return "紧要介入提醒", name + "需要您介入处理，点击查看"
	}
	return "接待分析已完成", name + "的接待分析已完成，点击查看"
}

// pushSalutation 生成推送用称呼：姓氏 + 先生/女士，绝不输出客户全名。
func pushSalutation(name, gender string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "客户") {
		return "客户"
	}
	runes := []rune(name)
	if len(runes) == 0 {
		return "客户"
	}
	suffix := "先生"
	if isFemaleGender(gender) {
		suffix = "女士"
	}
	switch {
	case strings.HasSuffix(name, "女士"):
		suffix = "女士"
	case strings.HasSuffix(name, "先生"):
		suffix = "先生"
	}
	return string(runes[0]) + suffix
}

func isFemaleGender(gender string) bool {
	lower := strings.ToLower(strings.TrimSpace(gender))
	return strings.Contains(lower, "女") || strings.Contains(lower, "female") || strings.Contains(lower, "f")
}
