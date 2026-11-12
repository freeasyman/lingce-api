package his

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// 检查（PACS）业务常量与纯逻辑校验。
// 数据模型基线：docs/his/15-检验检查数据模型.md「二、检查（PACS）」。
const (
	examStatusPending    = "pending"     // 待检查
	examStatusInProgress = "in_progress" // 检查中
	examStatusReported   = "reported"    // 已出报告
	examStatusCancelled  = "cancelled"   // 已取消

	examActionStart  = "start"
	examActionReport = "report"
	examActionCancel = "cancel"

	maxExamItemRunes       = 64
	maxExamBodyPartRunes   = 64
	maxExamPurposeRunes    = 200
	maxExamFindingsRunes   = 4000
	maxExamImpressionRunes = 2000
	maxExamSuggestionRunes = 2000
	maxExamImageRefs       = 20
	maxExamImageURLRunes   = 500
	maxExamKeywordRunes    = 64

	examDateLayout = "2006-01-02"
)

// examRequestNoFormat 检查申请单号：E + 日期 + 4 位流水（his_number_counters scope='exam'）。
func examRequestNoFormat(seq int64, at time.Time) string {
	return fmt.Sprintf("E%s%04d", at.Format("20060102"), seq)
}

// trimExamText 清洗文本字段：去首尾空白，按字符数限制长度。
func trimExamText(value string, maxRunes int) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxRunes {
		return "", ErrInvalidRequest
	}
	return value, nil
}

// normalizeExamStatusFilter 检查工作站页签状态映射：空值表示全部。
func normalizeExamStatusFilter(status string) ([]string, error) {
	switch strings.TrimSpace(status) {
	case "":
		return nil, nil
	case examStatusPending:
		return []string{examStatusPending}, nil
	case examStatusInProgress:
		return []string{examStatusInProgress}, nil
	case examStatusReported:
		return []string{examStatusReported}, nil
	case examStatusCancelled:
		return []string{examStatusCancelled}, nil
	default:
		return nil, ErrInvalidRequest
	}
}

// normalizeExamDate 校验开单日期（YYYY-MM-DD），空值表示不筛选。
func normalizeExamDate(date string) (string, error) {
	date = strings.TrimSpace(date)
	if date == "" {
		return "", nil
	}
	parsed, err := time.Parse(examDateLayout, date)
	if err != nil {
		return "", ErrInvalidRequest
	}
	return parsed.Format(examDateLayout), nil
}

// normalizeExamKeyword 清洗列表检索词：患者/病案号/项目/部位。
func normalizeExamKeyword(keyword string) (string, error) {
	return trimExamText(keyword, maxExamKeywordRunes)
}

// normalizeCreateExamRequest 校验并清洗开检查申请入参。
func normalizeCreateExamRequest(req CreateExamRequestRequest) (CreateExamRequestRequest, error) {
	if req.EncounterID <= 0 {
		return req, ErrInvalidRequest
	}
	itemName, err := trimExamText(req.ItemName, maxExamItemRunes)
	if err != nil || itemName == "" {
		return req, ErrInvalidRequest
	}
	bodyPart, err := trimExamText(req.BodyPart, maxExamBodyPartRunes)
	if err != nil {
		return req, ErrInvalidRequest
	}
	purpose, err := trimExamText(req.Purpose, maxExamPurposeRunes)
	if err != nil {
		return req, ErrInvalidRequest
	}
	req.ItemName = itemName
	req.BodyPart = bodyPart
	req.Purpose = purpose
	// 收费项目可选：0 表示未选（按项目名自动匹配），负数直接拒绝。
	if err := normalizeChargeItemID(req.ChargeItemID); err != nil {
		return req, err
	}
	return req, nil
}

// normalizeExamImageRefs 校验影像引用：最多 20 条、每条 URL ≤500 字符；空白项忽略。
func normalizeExamImageRefs(refs []string) ([]string, error) {
	normalized := make([]string, 0, len(refs))
	for _, ref := range refs {
		trimmed, err := trimExamText(ref, maxExamImageURLRunes)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) > maxExamImageRefs {
		return nil, ErrInvalidRequest
	}
	return normalized, nil
}

// normalizeReportExamRequest 校验并清洗报告入参：印象必填。
func normalizeReportExamRequest(req ReportExamRequestRequest) (ReportExamRequestRequest, error) {
	findings, err := trimExamText(req.Findings, maxExamFindingsRunes)
	if err != nil {
		return req, ErrInvalidRequest
	}
	impression, err := trimExamText(req.Impression, maxExamImpressionRunes)
	if err != nil || impression == "" {
		return req, ErrInvalidRequest
	}
	suggestion, err := trimExamText(req.Suggestion, maxExamSuggestionRunes)
	if err != nil {
		return req, ErrInvalidRequest
	}
	imageRefs, err := normalizeExamImageRefs(req.ImageRefs)
	if err != nil {
		return req, ErrInvalidRequest
	}
	req.Findings = findings
	req.Impression = impression
	req.Suggestion = suggestion
	req.ImageRefs = imageRefs
	return req, nil
}

// examTransitionAllowed 检查状态机：
// start 仅 pending；report 仅 in_progress；cancel 仅 pending。
func examTransitionAllowed(status, action string) error {
	switch action {
	case examActionStart:
		if status != examStatusPending {
			return ErrInvalidState
		}
	case examActionReport:
		if status != examStatusInProgress {
			return ErrInvalidState
		}
	case examActionCancel:
		if status != examStatusPending {
			return ErrInvalidState
		}
	default:
		return ErrInvalidRequest
	}
	return nil
}

// nextExamStatus 返回动作执行后的目标状态。
func nextExamStatus(action string) (string, error) {
	switch action {
	case examActionStart:
		return examStatusInProgress, nil
	case examActionReport:
		return examStatusReported, nil
	case examActionCancel:
		return examStatusCancelled, nil
	default:
		return "", ErrInvalidRequest
	}
}

// ---------------------------------------------------------------------------
// Service：检查工作站用例（校验 → Store）

// CreateExamRequest 开检查申请（校验 encounter 属于本租户在 Store 内完成）。
func (s *Service) CreateExamRequest(ctx context.Context, tenantID, createdBy int64, req CreateExamRequestRequest) (ExamRequest, error) {
	normalized, err := normalizeCreateExamRequest(req)
	if err != nil {
		return ExamRequest{}, err
	}
	return s.store.CreateExamRequest(ctx, tenantID, createdBy, normalized)
}

// ListExamRequests 检查工作站列表：status/date/keyword 均可为空。
func (s *Service) ListExamRequests(ctx context.Context, tenantID int64, status, date, keyword string) ([]ExamRequest, error) {
	statuses, err := normalizeExamStatusFilter(status)
	if err != nil {
		return nil, err
	}
	normalizedDate, err := normalizeExamDate(date)
	if err != nil {
		return nil, err
	}
	normalizedKeyword, err := normalizeExamKeyword(keyword)
	if err != nil {
		return nil, err
	}
	return s.store.ListExamRequests(ctx, tenantID, statuses, normalizedDate, normalizedKeyword)
}

// GetExamRequest 检查申请详情（含报告）。
func (s *Service) GetExamRequest(ctx context.Context, tenantID, id int64) (ExamRequest, error) {
	if id <= 0 {
		return ExamRequest{}, ErrInvalidRequest
	}
	return s.store.GetExamRequest(ctx, tenantID, id)
}

// StartExamRequest 开始检查：pending → in_progress。
func (s *Service) StartExamRequest(ctx context.Context, tenantID, id int64) (ExamRequest, error) {
	if id <= 0 {
		return ExamRequest{}, ErrInvalidRequest
	}
	return s.store.StartExamRequest(ctx, tenantID, id)
}

// ReportExamRequest 录入报告：in_progress → reported（事务内 upsert 报告）。
func (s *Service) ReportExamRequest(ctx context.Context, tenantID, reportedBy, id int64, req ReportExamRequestRequest) (ExamRequest, error) {
	if id <= 0 {
		return ExamRequest{}, ErrInvalidRequest
	}
	normalized, err := normalizeReportExamRequest(req)
	if err != nil {
		return ExamRequest{}, err
	}
	return s.store.ReportExamRequest(ctx, tenantID, reportedBy, id, normalized)
}

// CancelExamRequest 取消申请：仅 pending。
func (s *Service) CancelExamRequest(ctx context.Context, tenantID, id int64) (ExamRequest, error) {
	if id <= 0 {
		return ExamRequest{}, ErrInvalidRequest
	}
	return s.store.CancelExamRequest(ctx, tenantID, id)
}
