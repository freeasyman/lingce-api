package opportunityalert

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
)

type Service struct {
	store              *Store
	messageSender      MessageSender
	employeeWebBaseURL string
}

func NewService(store *Store, messageSender MessageSender, employeeWebBaseURL string) *Service {
	return &Service{store: store, messageSender: messageSender, employeeWebBaseURL: strings.TrimRight(strings.TrimSpace(employeeWebBaseURL), "/")}
}

func (s *Service) ListForAdmin(ctx context.Context, claims *auth.Claims, req AdminListRequest) ([]*AlertResponse, int, error) {
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		return nil, 0, fmt.Errorf("forbidden")
	}
	items, total, err := s.store.ListForAdmin(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	resp := make([]*AlertResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, ToResponse(item))
	}
	return resp, total, nil
}

func (s *Service) GetForAdmin(ctx context.Context, claims *auth.Claims, alertID int64) (*AlertResponse, error) {
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		return nil, fmt.Errorf("forbidden")
	}
	item, err := s.store.GetByID(ctx, alertID)
	if err != nil {
		return nil, err
	}
	return ToResponse(item), nil
}

func (s *Service) ListRecipientsForAdmin(ctx context.Context, claims *auth.Claims, alertID int64) ([]*AlertRecipientResponse, error) {
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		return nil, fmt.Errorf("forbidden")
	}
	items, err := s.store.ListRecipients(ctx, alertID)
	if err != nil {
		return nil, err
	}
	resp := make([]*AlertRecipientResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, ToRecipientResponse(item))
	}
	return resp, nil
}

func (s *Service) ListDeliveryLogsForAdmin(ctx context.Context, claims *auth.Claims, alertID int64) ([]*AlertDeliveryLogResponse, error) {
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		return nil, fmt.Errorf("forbidden")
	}
	items, err := s.store.ListDeliveryLogs(ctx, alertID)
	if err != nil {
		return nil, err
	}
	resp := make([]*AlertDeliveryLogResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, ToDeliveryLogResponse(item))
	}
	return resp, nil
}

func (s *Service) ListRecentDeliveriesForAdmin(ctx context.Context, claims *auth.Claims, req RecentDeliveriesRequest) ([]*RecentDeliveryResponse, error) {
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		return nil, fmt.Errorf("forbidden")
	}
	items, err := s.store.ListRecentDeliveries(ctx, req)
	if err != nil {
		return nil, err
	}
	resp := make([]*RecentDeliveryResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, ToRecentDeliveryResponse(item))
	}
	return resp, nil
}

func (s *Service) ListForEmployee(ctx context.Context, claims *auth.Claims, req ListRequest) ([]*AlertResponse, int, error) {
	tenantID, employeeID, err := employeeScope(claims)
	if err != nil {
		return nil, 0, err
	}
	items, total, err := s.store.ListForEmployee(ctx, tenantID, employeeID, req)
	if err != nil {
		return nil, 0, err
	}
	resp := make([]*AlertResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, ToResponse(item))
	}
	return resp, total, nil
}

func (s *Service) GetForEmployee(ctx context.Context, claims *auth.Claims, alertID int64) (*AlertResponse, error) {
	tenantID, employeeID, err := employeeScope(claims)
	if err != nil {
		return nil, err
	}
	item, err := s.store.GetForEmployee(ctx, tenantID, employeeID, alertID)
	if err != nil {
		return nil, err
	}
	return ToResponse(item), nil
}

func (s *Service) MarkViewed(ctx context.Context, claims *auth.Claims, alertID int64) (*AlertResponse, error) {
	tenantID, employeeID, err := employeeScope(claims)
	if err != nil {
		return nil, err
	}
	item, err := s.store.MarkViewed(ctx, tenantID, employeeID, alertID)
	if err != nil {
		return nil, err
	}
	return ToResponse(item), nil
}

func (s *Service) MarkHandled(ctx context.Context, claims *auth.Claims, alertID int64) (*AlertResponse, error) {
	tenantID, employeeID, err := employeeScope(claims)
	if err != nil {
		return nil, err
	}
	item, err := s.store.GetForEmployee(ctx, tenantID, employeeID, alertID)
	if err != nil {
		return nil, err
	}
	if item.EmployeeID != employeeID {
		return nil, fmt.Errorf("only alert owner can handle this alert")
	}
	updated, err := s.store.MarkHandled(ctx, tenantID, employeeID, alertID)
	if err != nil {
		return nil, err
	}
	return ToResponse(updated), nil
}

func (s *Service) MarkIgnored(ctx context.Context, claims *auth.Claims, alertID int64) (*AlertResponse, error) {
	tenantID, employeeID, err := employeeScope(claims)
	if err != nil {
		return nil, err
	}
	item, err := s.store.GetForEmployee(ctx, tenantID, employeeID, alertID)
	if err != nil {
		return nil, err
	}
	if item.EmployeeID != employeeID {
		return nil, fmt.Errorf("only alert owner can ignore this alert")
	}
	updated, err := s.store.MarkIgnored(ctx, tenantID, employeeID, alertID)
	if err != nil {
		return nil, err
	}
	return ToResponse(updated), nil
}

// employeeScope resolves the tenant and employee identity for employee/mobile callers.
//
// Opportunity alerts are strictly employee-scoped. This helper rejects admin tokens
// and tokens without both tenant_id and employee_id, so the rest of the service can
// assume the returned pair is valid.
func employeeScope(claims *auth.Claims) (int64, int64, error) {
	if claims == nil {
		return 0, 0, fmt.Errorf("invalid token")
	}
	if claims.UserType != auth.UserTypeEmployee && claims.UserType != auth.UserTypeMobile {
		return 0, 0, fmt.Errorf("forbidden")
	}
	if claims.TenantID == nil || *claims.TenantID <= 0 {
		return 0, 0, fmt.Errorf("tenant_id missing in token")
	}
	if claims.UserID <= 0 {
		return 0, 0, fmt.Errorf("employee_id missing in token")
	}
	return *claims.TenantID, claims.UserID, nil
}

func normalizeStatus(status string) string {
	status = strings.TrimSpace(status)
	switch status {
	case "pending", "viewed", "handled", "ignored":
		return status
	default:
		return ""
	}
}

func normalizeDeliveryResult(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "delivered", "undelivered":
		return value
	default:
		return ""
	}
}

func (s *Service) CreateAlert(ctx context.Context, input CreateAlertInput) (*AlertResponse, bool, error) {
	if strings.TrimSpace(input.DedupeKey) == "" {
		input.DedupeKey = fmt.Sprintf("tenant:%d:recording:%d:type:%s", input.TenantID, input.RecordingID, input.AlertType)
	}
	if strings.TrimSpace(input.AlertType) == "" {
		input.AlertType = "opportunity_not_advanced"
	}
	if strings.TrimSpace(input.Title) == "" {
		input.Title = "发现一条可补救的成交机会"
	}
	ccIDs, err := s.store.ListActiveCCEmployeeIDs(ctx, input.TenantID, input.EmployeeID)
	if err != nil {
		return nil, false, err
	}
	item, created, err := s.store.Create(ctx, input, ccIDs)
	if err != nil {
		return nil, false, err
	}
	if created {
		_ = s.SendWeCom(ctx, item.ID)
	}
	return ToResponse(item), created, nil
}

func (s *Service) SendWeCom(ctx context.Context, alertID int64) error {
	if s.messageSender == nil || !s.messageSender.IsEnabled() {
		return nil
	}
	item, err := s.store.GetByID(ctx, alertID)
	if err != nil {
		return err
	}
	recipientIDs, err := s.store.ListRecipientEmployeeIDs(ctx, alertID)
	if err != nil {
		return err
	}
	if len(recipientIDs) == 0 || strings.TrimSpace(s.employeeWebBaseURL) == "" {
		return nil
	}
	dedupeKey := fmt.Sprintf("opportunity_alert:%d", item.ID)
	content := strings.TrimSpace(item.Summary)
	if content == "" {
		content = strings.TrimSpace(item.Reason)
	}
	if content == "" {
		content = "这通沟通里发现一条疑似可补救机会，请及时查看建议。"
	}
	err = s.messageSender.SendInternalMessage(ctx, MessageSendRequest{
		MessageScene: "opportunity_alert",
		DedupeKey:    dedupeKey,
		EmployeeIDs:  recipientIDs,
		Title:        item.Title,
		Content:      content,
		TargetURL:    fmt.Sprintf("%s/opportunity-alerts/%d", s.employeeWebBaseURL, item.ID),
		ButtonText:   "查看补救建议",
		Extra: map[string]any{
			"alert_id":     item.ID,
			"recording_id": item.RecordingID,
			"alert_type":   item.AlertType,
		},
	})
	if syncErr := s.store.SyncWeComDeliveryStatuses(ctx, item.ID, dedupeKey); syncErr != nil && err == nil {
		err = syncErr
	}
	return err
}

func (s *Service) ResendWeCom(ctx context.Context, alertID int64, employeeIDs []int64) error {
	if s.messageSender == nil || !s.messageSender.IsEnabled() {
		return nil
	}
	item, err := s.store.GetByID(ctx, alertID)
	if err != nil {
		return err
	}
	recipientIDs, err := s.store.ListRecipientEmployeeIDs(ctx, alertID)
	if err != nil {
		return err
	}
	if len(recipientIDs) == 0 || strings.TrimSpace(s.employeeWebBaseURL) == "" {
		return nil
	}
	targetIDs := recipientIDs
	if len(employeeIDs) > 0 {
		allowed := make(map[int64]struct{}, len(recipientIDs))
		for _, id := range recipientIDs {
			allowed[id] = struct{}{}
		}
		filtered := make([]int64, 0, len(employeeIDs))
		seen := make(map[int64]struct{}, len(employeeIDs))
		for _, id := range employeeIDs {
			if _, ok := allowed[id]; !ok || id <= 0 {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			filtered = append(filtered, id)
		}
		targetIDs = filtered
	}
	if len(targetIDs) == 0 {
		return fmt.Errorf("no eligible recipients to resend")
	}
	content := strings.TrimSpace(item.Summary)
	if content == "" {
		content = strings.TrimSpace(item.Reason)
	}
	if content == "" {
		content = "这通沟通里发现一条疑似可补救机会，请及时查看建议。"
	}
	dedupeKey := fmt.Sprintf("opportunity_alert:%d:resend:%d", item.ID, time.Now().UnixNano())
	err = s.messageSender.SendInternalMessage(ctx, MessageSendRequest{
		MessageScene: "opportunity_alert",
		DedupeKey:    dedupeKey,
		EmployeeIDs:  targetIDs,
		Title:        item.Title,
		Content:      content,
		TargetURL:    fmt.Sprintf("%s/opportunity-alerts/%d", s.employeeWebBaseURL, item.ID),
		ButtonText:   "查看补救建议",
		Extra: map[string]any{
			"alert_id":     item.ID,
			"recording_id": item.RecordingID,
			"alert_type":   item.AlertType,
			"resend":       true,
		},
	})
	if syncErr := s.store.SyncWeComDeliveryStatusesForEmployees(ctx, item.ID, dedupeKey, targetIDs); syncErr != nil && err == nil {
		err = syncErr
	}
	return err
}

func (s *Service) AutoResendSkippedUnboundForEmployee(ctx context.Context, tenantID, employeeID int64) error {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil
	}
	if err := tenancy.RequirePositiveID("employee_id", employeeID); err != nil {
		return nil
	}
	alertIDs, err := s.store.ListSkippedUnboundAlertIDsByEmployee(ctx, tenantID, employeeID, 20)
	if err != nil {
		return err
	}
	for _, alertID := range alertIDs {
		if err := s.ResendWeCom(ctx, alertID, []int64{employeeID}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CreateFromRecording(ctx context.Context, recordingID int64, triggerSource string) error {
	source, err := s.store.GetRecordingAlertSource(ctx, recordingID)
	if err != nil {
		return err
	}
	candidate, ok := extractCandidate(source)
	if !ok {
		return nil
	}
	candidate.RawPayload["trigger_source"] = triggerSource
	_, _, err = s.CreateAlert(ctx, CreateAlertInput{
		TenantID:          source.TenantID,
		RecordingID:       source.ID,
		EmployeeID:        source.EmployeeID,
		CustomerID:        source.CustomerID,
		CustomerName:      source.CustomerName,
		AlertType:         candidate.AlertType,
		Title:             candidate.Title,
		Summary:           candidate.Summary,
		Reason:            candidate.Reason,
		CustomerObjection: candidate.CustomerObjection,
		Evidence:          candidate.Evidence,
		SuggestedAction:   candidate.SuggestedAction,
		SuggestedScript:   candidate.SuggestedScript,
		Priority:          candidate.Priority,
		DedupeKey:         fmt.Sprintf("tenant:%d:recording:%d:type:%s", source.TenantID, source.ID, candidate.AlertType),
		RawPayload:        candidate.RawPayload,
	})
	return err
}

type alertCandidate struct {
	AlertType         string
	Title             string
	Summary           string
	Reason            string
	CustomerObjection string
	Evidence          string
	SuggestedAction   string
	SuggestedScript   string
	Priority          string
	RawPayload        JSONObject
}

func extractCandidate(source *RecordingAlertSource) (*alertCandidate, bool) {
	if source == nil || len(source.AnalysisResult) == 0 {
		return nil, false
	}
	analysis := source.AnalysisResult
	dealOutcome := pickMap(analysis, "deal_outcome")
	persuasive := pickMap(analysis, "persuasive")
	analysisSummary := pickMap(analysis, "analysis_summary")
	concernStrategy := pickMap(dealOutcome, "concern_strategy")

	dealResult := firstText(
		dealOutcome["result"],
		persuasive["deal_status"],
		analysis["deal_status"],
	)
	if !strings.Contains(dealResult, "意向明确未成交") {
		return nil, false
	}

	reason := firstText(
		concernStrategy["stuck_point_headline"],
		concernStrategy["real_reason"],
		analysisSummary["conversion_opportunity"],
		analysis["conversion_opportunity"],
		persuasive["stuck_point_headline"],
	)
	evidence := firstText(
		dealOutcome["evidence"],
		concernStrategy["evidence"],
		analysis["status_summary"],
		analysis["conversation_summary"],
	)
	objection := firstText(
		concernStrategy["real_reason"],
		persuasive["real_concern"],
		persuasive["real_reason"],
		analysis["core_blockers"],
	)
	action := firstText(
		concernStrategy["next_action"],
		analysisSummary["next_action"],
		analysisSummary["conversion_opportunity"],
		analysis["follow_up_advice"],
	)
	script := firstText(
		concernStrategy["script"],
		concernStrategy["suggested_script"],
		analysisSummary["suggested_script"],
	)
	if action == "" {
		action = "建议尽快联系客户，先接住顾虑，再明确约定下一步到院、复查或方案确认时间。"
	}
	if script == "" {
		script = "我刚复盘了一下您的情况，您不是没有需求，主要是还有几个顾虑没完全放下。我们可以先把您最担心的点讲清楚，再一起确认下一步怎么安排。"
	}
	summary := firstText(
		analysis["status_summary"],
		analysis["conversation_summary"],
		evidence,
	)
	if summary == "" {
		summary = "客户表达过明确意向，但本次沟通没有锁定下一步，存在可补救机会。"
	}
	if reason == "" {
		reason = "分析结果显示客户并非明确拒绝，而是带着顾虑离开，需要尽快二次跟进。"
	}
	return &alertCandidate{
		AlertType:         "opportunity_not_advanced",
		Title:             "客户有明确意向，但没有锁定下一步",
		Summary:           summary,
		Reason:            reason,
		CustomerObjection: objection,
		Evidence:          evidence,
		SuggestedAction:   action,
		SuggestedScript:   script,
		Priority:          "high",
		RawPayload: JSONObject{
			"deal_result":      dealResult,
			"deal_outcome":     dealOutcome,
			"persuasive":       persuasive,
			"analysis_summary": analysisSummary,
		},
	}, true
}

func pickMap(source map[string]interface{}, key string) map[string]interface{} {
	if source == nil {
		return map[string]interface{}{}
	}
	if value, ok := source[key].(map[string]interface{}); ok {
		return value
	}
	return map[string]interface{}{}
}

func firstText(values ...interface{}) string {
	for _, value := range values {
		switch v := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		case []interface{}:
			parts := make([]string, 0, len(v))
			for _, item := range v {
				if text := firstText(item); text != "" {
					parts = append(parts, text)
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "；")
			}
		case []string:
			parts := make([]string, 0, len(v))
			for _, item := range v {
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					parts = append(parts, trimmed)
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "；")
			}
		case map[string]interface{}:
			for _, key := range []string{"text", "content", "summary", "reason", "value"} {
				if text := firstText(v[key]); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func (s *Service) ListCCRules(ctx context.Context, claims *auth.Claims, tenantID int64, employeeID *int64) ([]*CCRuleResponse, error) {
	tid, err := resolveConfigTenantID(claims, tenantID)
	if err != nil {
		return nil, err
	}
	items, err := s.store.ListCCRules(ctx, tid, employeeID)
	if err != nil {
		return nil, err
	}
	resp := make([]*CCRuleResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, toCCRuleResponse(item))
	}
	return resp, nil
}

func (s *Service) CreateCCRule(ctx context.Context, claims *auth.Claims, req CreateCCRuleRequest) (*CCRuleResponse, error) {
	tid, err := resolveConfigTenantID(claims, req.TenantID)
	if err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("employee_id", req.EmployeeID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("cc_employee_id", req.CCEmployeeID); err != nil {
		return nil, err
	}
	if req.EmployeeID == req.CCEmployeeID {
		return nil, fmt.Errorf("employee and cc employee cannot be the same")
	}
	item, err := s.store.UpsertCCRule(ctx, tid, req.EmployeeID, req.CCEmployeeID)
	if err != nil {
		return nil, err
	}
	return toCCRuleResponse(item), nil
}

func (s *Service) DeleteCCRule(ctx context.Context, claims *auth.Claims, tenantID, ruleID int64) error {
	tid, err := resolveConfigTenantID(claims, tenantID)
	if err != nil {
		return err
	}
	return s.store.DeleteCCRule(ctx, tid, ruleID)
}

// resolveConfigTenantID resolves the tenant used by admin config operations.
//
// Tenant-bound admin tokens use their embedded tenant directly. Otherwise, the
// caller must provide an explicit tenant_id so the operation never runs against
// an ambiguous tenant context.
func resolveConfigTenantID(claims *auth.Claims, requestedTenantID int64) (int64, error) {
	if claims == nil {
		return 0, fmt.Errorf("invalid token")
	}
	if claims.TenantID != nil && *claims.TenantID > 0 {
		return *claims.TenantID, nil
	}
	if claims.UserType == auth.UserTypeEmployee || claims.UserType == auth.UserTypeMobile {
		return 0, fmt.Errorf("tenant_id missing in token")
	}
	if err := tenancy.RequirePositiveID("tenant_id", requestedTenantID); err != nil {
		return 0, err
	}
	return requestedTenantID, nil
}

func toCCRuleResponse(item *CCRule) *CCRuleResponse {
	if item == nil {
		return nil
	}
	return &CCRuleResponse{
		ID:             item.ID,
		TenantID:       item.TenantID,
		EmployeeID:     item.EmployeeID,
		EmployeeName:   item.EmployeeName,
		CCEmployeeID:   item.CCEmployeeID,
		CCEmployeeName: item.CCEmployeeName,
		IsActive:       item.IsActive,
		CreatedAt:      formatTime(item.CreatedAt),
		UpdatedAt:      formatTime(item.UpdatedAt),
	}
}
