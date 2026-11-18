package dealexpush

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/config"
)

var (
	// ErrEmailEventNotFound 表示推送事件不存在或不属于当前租户。
	ErrEmailEventNotFound = errors.New("email event not found")
	// ErrEmailDisabled 表示 [deal.email].enabled=false，只允许干跑，不允许发信。
	ErrEmailDisabled = errors.New("deal email is disabled")
	// ErrEmailSMTPNotConfigured 表示既没有 [deal.email] SMTP，也没有 external.ticket_notify 可回退。
	ErrEmailSMTPNotConfigured = errors.New("smtp is not configured")
)

// emailRecipient 是解析后的单个收件人（含被过滤原因）。
type emailRecipient struct {
	EmployeeID int64
	Name       string
	Email      string
	Kind       string // to | cc
	Skip       string // 非空表示不发送：no_email / not_in_whitelist / employee_not_found
}

// EmailDeliveryResult 是单收件人投递结果（API 返回）。
type EmailDeliveryResult struct {
	RecipientEmployeeID int64  `json:"recipient_employee_id"`
	Email               string `json:"email"`
	Kind                string `json:"kind"`
	Status              string `json:"status"`
	MessageID           string `json:"message_id"`
	Error               string `json:"error"`
}

// ResolveSMTPConfig 合并 [deal.email] 与 external.ticket_notify：deal.email 为空时逐字段回退。
func ResolveSMTPConfig(cfg config.DealEmailConfig, fallback config.TicketNotifyConfig) SMTPConfig {
	host := firstNonEmpty(cfg.SMTPHost, fallback.SMTPHost)
	port := cfg.SMTPPort
	if port <= 0 {
		port = fallback.SMTPPort
	}
	if port <= 0 {
		port = 465
	}
	user := firstNonEmpty(cfg.SMTPUser, fallback.SMTPUser)
	pass := firstNonEmpty(cfg.SMTPPass, fallback.SMTPPass)
	from := firstNonEmpty(cfg.From, fallback.From, user)
	return SMTPConfig{Host: host, Port: port, User: user, Pass: pass, From: from}
}

func missingSMTPConfigFields(cfg config.DealEmailConfig, fallback config.TicketNotifyConfig) []string {
	resolved := ResolveSMTPConfig(cfg, fallback)
	var missing []string
	if resolved.Host == "" {
		missing = append(missing, "smtp_host")
	}
	if resolved.Port <= 0 {
		missing = append(missing, "smtp_port")
	}
	if resolved.User == "" {
		missing = append(missing, "smtp_user")
	}
	if resolved.Pass == "" {
		missing = append(missing, "smtp_pass")
	}
	if resolved.From == "" {
		missing = append(missing, "from")
	}
	return missing
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// sendEmailForEvent 在事件插入后触发邮件；失败只记录，不影响分析结果。
func (s *Service) sendEmailForEvent(ctx context.Context, eventID int64) {
	if s == nil || s.emailStore == nil {
		return
	}
	if !s.emailCfg.Enabled {
		return
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
	defer cancel()
	results, err := s.SendEventEmail(sendCtx, eventID, 0)
	if err != nil {
		s.log.Warn("deal email: send failed", "event_id", eventID, "error", err)
		return
	}
	sent, failed, skipped := 0, 0, 0
	for _, item := range results {
		switch item.Status {
		case "sent":
			sent++
		case "failed":
			failed++
		default:
			skipped++
		}
	}
	s.log.Info("deal email: event processed",
		"event_id", eventID, "sent", sent, "failed", failed, "skipped", skipped)
}

// SendEventEmail 解析租户内路由并为每个收件人单独发一封邮件。
// tenantID>0 时校验事件归属；返回逐收件人结果，单收件人失败不阻断其他人。
func (s *Service) SendEventEmail(ctx context.Context, eventID, tenantID int64) ([]*EmailDeliveryResult, error) {
	if s == nil || s.emailStore == nil {
		return nil, fmt.Errorf("email service is not configured")
	}
	if !s.emailCfg.Enabled {
		return nil, ErrEmailDisabled
	}
	if eventID <= 0 {
		return nil, ErrEmailEventNotFound
	}
	event, err := s.emailStore.LoadEmailEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if tenantID > 0 && event.TenantID != tenantID {
		return nil, ErrEmailEventNotFound
	}

	analysis, err := s.emailStore.LoadAnalysisCopy(ctx, event.TenantID, event.RecordingID, event.AnalysisVersion)
	if err != nil {
		// 分析文案缺失不阻断发送，使用默认文案。
		s.log.Warn("deal email: load analysis copy failed", "event_id", eventID, "error", err)
		analysis = &AnalysisCopy{}
	}

	recipients, err := s.resolveEmailRecipients(ctx, event)
	if err != nil {
		return nil, err
	}

	results := make([]*EmailDeliveryResult, 0, len(recipients))
	for _, recipient := range recipients {
		result, err := s.deliverEmail(ctx, event, analysis, recipient)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}

	// 没有任何候选收件人（例如默认负责人不存在）时记一条事件级 skipped，便于排查。
	if len(recipients) == 0 {
		delivery := &EmailDelivery{
			TenantID: event.TenantID, EventID: event.ID, RecipientEmployeeID: 0,
			Email: "", Kind: "to", Status: "skipped", Error: "no_recipients",
		}
		if err := s.emailStore.SaveDelivery(ctx, delivery); err != nil {
			return results, err
		}
		results = append(results, &EmailDeliveryResult{
			RecipientEmployeeID: 0, Email: "", Kind: "to", Status: "skipped", Error: "no_recipients",
		})
	}
	return results, nil
}

// resolveEmailRecipients 执行租户内路由：先取配置，再按默认收件人补齐，最后过滤邮箱/白名单。
func (s *Service) resolveEmailRecipients(ctx context.Context, event *EmailEvent) ([]emailRecipient, error) {
	if event == nil {
		return nil, nil
	}
	eventType := normalizeEventType(event.RecipientRole)

	// 咨询师事件 owner=事件收件人；负责人事件 owner=该接待的咨询师。
	ownerID := event.RecipientEmployeeID
	if eventType == "manager" {
		ownerID = event.ConsultantEmployeeID
	}
	if ownerID <= 0 {
		return nil, nil
	}

	route, err := s.emailStore.Route(ctx, event.TenantID, ownerID, eventType)
	if err != nil {
		return nil, err
	}
	toIDs, ccIDs := []int64{}, []int64{}
	if route != nil {
		toIDs = route.ToEmployeeIDs
		ccIDs = route.CcEmployeeIDs
	}
	if len(uniquePositive(toIDs)) == 0 {
		defaultIDs, err := s.defaultRecipientIDs(ctx, event, eventType, ownerID)
		if err != nil {
			return nil, err
		}
		toIDs = defaultIDs
	}

	toIDs = uniquePositive(toIDs)
	ccIDs = uniquePositive(ccIDs)
	toSet := make(map[int64]struct{}, len(toIDs))
	for _, id := range toIDs {
		toSet[id] = struct{}{}
	}
	dedupedCC := make([]int64, 0, len(ccIDs))
	for _, id := range ccIDs {
		if _, ok := toSet[id]; ok {
			continue
		}
		dedupedCC = append(dedupedCC, id)
	}

	recipients := make([]emailRecipient, 0, len(toIDs)+len(dedupedCC))
	recipients = append(recipients, s.buildRecipients(ctx, event.TenantID, toIDs, "to")...)
	recipients = append(recipients, s.buildRecipients(ctx, event.TenantID, dedupedCC, "cc")...)
	return recipients, nil
}

func (s *Service) defaultRecipientIDs(ctx context.Context, event *EmailEvent, eventType string, ownerID int64) ([]int64, error) {
	if eventType != "manager" {
		return []int64{ownerID}, nil
	}
	managerID, err := s.emailStore.DefaultManager(ctx, event.TenantID, ownerID)
	if err != nil {
		return nil, err
	}
	if managerID <= 0 {
		return nil, nil
	}
	return []int64{managerID}, nil
}

// buildRecipients 读取员工邮箱并按白名单过滤；被过滤的收件人保留原因用于记录 skipped。
func (s *Service) buildRecipients(ctx context.Context, tenantID int64, employeeIDs []int64, kind string) []emailRecipient {
	recipients := make([]emailRecipient, 0, len(employeeIDs))
	for _, employeeID := range employeeIDs {
		employee, err := s.emailStore.Employee(ctx, tenantID, employeeID)
		if err != nil {
			recipients = append(recipients, emailRecipient{EmployeeID: employeeID, Kind: kind, Skip: "employee_lookup_failed"})
			continue
		}
		if employee == nil {
			recipients = append(recipients, emailRecipient{EmployeeID: employeeID, Kind: kind, Skip: "employee_not_found"})
			continue
		}
		recipient := emailRecipient{EmployeeID: employee.ID, Name: employee.Name, Email: employee.Email, Kind: kind}
		if strings.TrimSpace(employee.Email) == "" {
			recipient.Skip = "no_email"
		} else if !emailWhitelisted(s.emailCfg.Whitelist, employee.Email) {
			recipient.Skip = "not_in_whitelist"
		}
		recipients = append(recipients, recipient)
	}
	return recipients
}

func (s *Service) deliverEmail(ctx context.Context, event *EmailEvent, analysis *AnalysisCopy, recipient emailRecipient) (*EmailDeliveryResult, error) {
	existing, err := s.emailStore.Delivery(ctx, event.ID, recipient.EmployeeID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Status == "sent" {
		return &EmailDeliveryResult{
			RecipientEmployeeID: recipient.EmployeeID, Email: existing.Email,
			Kind: existing.Kind, Status: "sent", MessageID: existing.MessageID,
		}, nil
	}

	delivery := &EmailDelivery{
		TenantID: event.TenantID, EventID: event.ID,
		RecipientEmployeeID: recipient.EmployeeID, Email: recipient.Email, Kind: recipient.Kind,
	}
	if recipient.Skip != "" {
		delivery.Status = "skipped"
		delivery.Error = recipient.Skip
		if err := s.emailStore.SaveDelivery(ctx, delivery); err != nil {
			return nil, err
		}
		return resultFromDelivery(delivery), nil
	}
	if s.emailCfg.IsDryRun() {
		delivery.Status = "skipped"
		delivery.Error = "dry_run"
		if err := s.emailStore.SaveDelivery(ctx, delivery); err != nil {
			return nil, err
		}
		return resultFromDelivery(delivery), nil
	}
	if s.mailer == nil {
		delivery.Status = "failed"
		delivery.Error = "smtp_not_configured"
		if err := s.emailStore.SaveDelivery(ctx, delivery); err != nil {
			return nil, err
		}
		return resultFromDelivery(delivery), nil
	}

	code, err := s.emailStore.EnsureShortLink(ctx, event.TenantID, event.RecordingID, recipient.EmployeeID)
	if err != nil {
		delivery.Status = "failed"
		delivery.Error = truncateString("create short link: "+err.Error(), 500)
		if saveErr := s.emailStore.SaveDelivery(ctx, delivery); saveErr != nil {
			return nil, saveErr
		}
		return resultFromDelivery(delivery), nil
	}

	eventType := normalizeEventType(event.RecipientRole)
	salutation := pushSalutation(event.CustomerName, event.CustomerGender)
	subject, text, htmlBody := buildEmailCopy(eventType, event.CustomerName, salutation, recipient.Name, analysis, s.emailLink(code))
	messageID, sendErr := s.mailer.Send(ctx, EmailMessage{
		From: s.emailFrom(), To: recipient.Email, Subject: subject, Text: text, HTML: htmlBody,
	})
	if sendErr != nil {
		delivery.Status = "failed"
		delivery.Error = truncateString(sendErr.Error(), 500)
		if saveErr := s.emailStore.SaveDelivery(ctx, delivery); saveErr != nil {
			return nil, saveErr
		}
		s.log.Warn("deal email: smtp send failed", "event_id", event.ID, "employee_id", recipient.EmployeeID, "error", sendErr)
		return resultFromDelivery(delivery), nil
	}
	now := time.Now()
	delivery.Status = "sent"
	delivery.MessageID = messageID
	delivery.SentAt = &now
	if err := s.emailStore.SaveDelivery(ctx, delivery); err != nil {
		return nil, err
	}
	s.log.Info("deal email: sent", "event_id", event.ID, "employee_id", recipient.EmployeeID, "kind", recipient.Kind, "message_id", messageID)
	return resultFromDelivery(delivery), nil
}

func (s *Service) emailFrom() string {
	return firstNonEmpty(s.emailCfg.From, s.ticketCfg.From)
}

func (s *Service) emailLink(code string) string {
	base := strings.TrimRight(strings.TrimSpace(s.emailCfg.LinkBase), "/")
	if base == "" {
		base = "https://khgl.xyz/s"
	}
	return base + "/" + strings.TrimSpace(code)
}

func resultFromDelivery(delivery *EmailDelivery) *EmailDeliveryResult {
	if delivery == nil {
		return nil
	}
	return &EmailDeliveryResult{
		RecipientEmployeeID: delivery.RecipientEmployeeID,
		Email:               delivery.Email,
		Kind:                delivery.Kind,
		Status:              delivery.Status,
		MessageID:           delivery.MessageID,
		Error:               delivery.Error,
	}
}

func normalizeEventType(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "manager", "intervention":
		return "manager"
	default:
		return "consultant"
	}
}

// emailWhitelisted 大小写不敏感地匹配邮箱白名单；空名单一律拒绝，避免误发。
func emailWhitelisted(whitelist []string, email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	for _, item := range whitelist {
		if strings.ToLower(strings.TrimSpace(item)) == email {
			return true
		}
	}
	return false
}

func uniquePositive(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

// ListAlertRoutes 返回租户路由配置和员工选择器数据；ownerEmployeeID>0 时只返回该 owner。
func (s *Service) ListAlertRoutes(ctx context.Context, tenantID, ownerEmployeeID int64) ([]*AlertRoute, []*EmailEmployee, error) {
	if s == nil || s.emailStore == nil {
		return nil, nil, fmt.Errorf("email service is not configured")
	}
	routes, err := s.emailStore.ListRoutes(ctx, tenantID, ownerEmployeeID)
	if err != nil {
		return nil, nil, err
	}
	employees, err := s.emailStore.ListEmployees(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	for _, route := range routes {
		if route.ToEmployeeIDs == nil {
			route.ToEmployeeIDs = []int64{}
		}
		if route.CcEmployeeIDs == nil {
			route.CcEmployeeIDs = []int64{}
		}
	}
	return routes, employees, nil
}

// UpsertAlertRoute 保存 owner+event_type 的收件人/抄送人配置。
func (s *Service) UpsertAlertRoute(ctx context.Context, tenantID, ownerEmployeeID int64, eventType string, to, cc []int64, actorID int64) (*AlertRoute, error) {
	if s == nil || s.emailStore == nil {
		return nil, fmt.Errorf("email service is not configured")
	}
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	if eventType != "consultant" && eventType != "manager" {
		return nil, fmt.Errorf("invalid event_type")
	}
	if ownerEmployeeID <= 0 {
		return nil, fmt.Errorf("owner_employee_id is required")
	}
	toIDs := uniquePositive(to)
	ccIDs := uniquePositive(cc)
	allIDs := append(append([]int64{}, toIDs...), ccIDs...)
	exists, err := s.emailStore.EmployeesExist(ctx, tenantID, append(allIDs, ownerEmployeeID))
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("employees must belong to current tenant")
	}
	route := &AlertRoute{
		TenantID: tenantID, OwnerEmployeeID: ownerEmployeeID, EventType: eventType,
		ToEmployeeIDs: toIDs, CcEmployeeIDs: ccIDs, CreatedBy: actorID, UpdatedBy: actorID,
	}
	saved, err := s.emailStore.UpsertRoute(ctx, route)
	if err != nil {
		return nil, err
	}
	saved.OwnerName, saved.OwnerEmail = s.ownerIdentity(ctx, tenantID, ownerEmployeeID)
	if saved.ToEmployeeIDs == nil {
		saved.ToEmployeeIDs = []int64{}
	}
	if saved.CcEmployeeIDs == nil {
		saved.CcEmployeeIDs = []int64{}
	}
	return saved, nil
}

func (s *Service) ownerIdentity(ctx context.Context, tenantID, employeeID int64) (string, string) {
	employee, err := s.emailStore.Employee(ctx, tenantID, employeeID)
	if err != nil || employee == nil {
		return "", ""
	}
	return employee.Name, employee.Email
}
