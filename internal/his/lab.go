package his

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// 检验（LIS）业务常量与纯逻辑校验。
// 数据模型基线：docs/his/15-检验检查数据模型.md「一、检验」。
const (
	labStatusPending   = "pending"   // 待采样
	labStatusSampled   = "sampled"   // 已采样
	labStatusReported  = "reported"  // 已出报告
	labStatusCancelled = "cancelled" // 已取消

	labActionSample = "sample"
	labActionReport = "report"
	labActionCancel = "cancel"

	maxLabItemNameRunes  = 64
	maxLabSpecimenRunes  = 32
	maxLabPurposeRunes   = 200
	maxLabResultRows     = 100
	maxLabCodeRunes      = 32
	maxLabValueRunes     = 64
	maxLabUnitRunes      = 32
	maxLabReferenceRunes = 64
	maxLabKeywordRunes   = 64

	labDateLayout = "2006-01-02"
)

// labRequestNoFormat 申请单号：L + 日期 + 4 位流水（his_number_counters scope='lab'）。
func labRequestNoFormat(seq int64, at time.Time) string {
	return fmt.Sprintf("L%s%04d", at.Format("20060102"), seq)
}

// trimLabText 清洗文本字段：去首尾空白，按字符数限制长度。
func trimLabText(value string, maxRunes int) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxRunes {
		return "", ErrInvalidRequest
	}
	return value, nil
}

// normalizeLabStatusFilter 检验工作站页签状态映射：空值表示全部。
func normalizeLabStatusFilter(status string) ([]string, error) {
	switch strings.TrimSpace(status) {
	case "":
		return nil, nil
	case labStatusPending:
		return []string{labStatusPending}, nil
	case labStatusSampled:
		return []string{labStatusSampled}, nil
	case labStatusReported:
		return []string{labStatusReported}, nil
	case labStatusCancelled:
		return []string{labStatusCancelled}, nil
	default:
		return nil, ErrInvalidRequest
	}
}

// normalizeLabDate 校验开单日期（YYYY-MM-DD），空值表示不筛选。
func normalizeLabDate(date string) (string, error) {
	date = strings.TrimSpace(date)
	if date == "" {
		return "", nil
	}
	parsed, err := time.Parse(labDateLayout, date)
	if err != nil {
		return "", ErrInvalidRequest
	}
	return parsed.Format(labDateLayout), nil
}

// normalizeLabKeyword 清洗列表检索词：患者/病案号/项目。
func normalizeLabKeyword(keyword string) (string, error) {
	return trimLabText(keyword, maxLabKeywordRunes)
}

// normalizeCreateLabRequest 校验并清洗开检验申请入参。
func normalizeCreateLabRequest(req CreateLabRequestRequest) (CreateLabRequestRequest, error) {
	if req.EncounterID <= 0 {
		return req, ErrInvalidRequest
	}
	itemName, err := trimLabText(req.ItemName, maxLabItemNameRunes)
	if err != nil || itemName == "" {
		return req, ErrInvalidRequest
	}
	specimenType, err := trimLabText(req.SpecimenType, maxLabSpecimenRunes)
	if err != nil {
		return req, ErrInvalidRequest
	}
	purpose, err := trimLabText(req.Purpose, maxLabPurposeRunes)
	if err != nil {
		return req, ErrInvalidRequest
	}
	req.ItemName = itemName
	req.SpecimenType = specimenType
	req.Purpose = purpose
	// 收费项目可选：0 表示未选（按项目名自动匹配），负数直接拒绝。
	if err := normalizeChargeItemID(req.ChargeItemID); err != nil {
		return req, err
	}
	return req, nil
}

// normalizeLabFlag 高低标志：H 偏高 / L 偏低 / 空 正常；N 视同正常。
func normalizeLabFlag(flag string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(flag))
	switch normalized {
	case "", "N":
		return "", nil
	case "H", "L":
		return normalized, nil
	default:
		return "", ErrInvalidRequest
	}
}

// normalizeLabResultItems 校验录入结果项：至少 1 条、最多 100 条，字段非空且限长。
func normalizeLabResultItems(items []LabResultItemInput) ([]LabResultItemInput, error) {
	if len(items) == 0 || len(items) > maxLabResultRows {
		return nil, ErrInvalidRequest
	}
	normalized := make([]LabResultItemInput, 0, len(items))
	for _, item := range items {
		code, err := trimLabText(item.ItemCode, maxLabCodeRunes)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		name, err := trimLabText(item.ItemName, maxLabItemNameRunes)
		if err != nil || name == "" {
			return nil, ErrInvalidRequest
		}
		value, err := trimLabText(item.Value, maxLabValueRunes)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		unit, err := trimLabText(item.Unit, maxLabUnitRunes)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		referenceRange, err := trimLabText(item.ReferenceRange, maxLabReferenceRunes)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		flag, err := normalizeLabFlag(item.Flag)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		normalized = append(normalized, LabResultItemInput{
			ItemCode:       code,
			ItemName:       name,
			Value:          value,
			Unit:           unit,
			ReferenceRange: referenceRange,
			Flag:           flag,
		})
	}
	return normalized, nil
}

// labTransitionAllowed 检验状态机：
// sample 仅 pending；report 仅 sampled；cancel 仅 pending。
func labTransitionAllowed(status, action string) error {
	switch action {
	case labActionSample:
		if status != labStatusPending {
			return ErrInvalidState
		}
	case labActionReport:
		if status != labStatusSampled {
			return ErrInvalidState
		}
	case labActionCancel:
		if status != labStatusPending {
			return ErrInvalidState
		}
	default:
		return ErrInvalidRequest
	}
	return nil
}

// nextLabStatus 返回动作执行后的目标状态。
func nextLabStatus(action string) (string, error) {
	switch action {
	case labActionSample:
		return labStatusSampled, nil
	case labActionReport:
		return labStatusReported, nil
	case labActionCancel:
		return labStatusCancelled, nil
	default:
		return "", ErrInvalidRequest
	}
}

// ---------------------------------------------------------------------------
// Service：检验工作站用例（校验 → Store）

// CreateLabRequest 开检验申请（校验 encounter 属于本租户在 Store 内完成）。
func (s *Service) CreateLabRequest(ctx context.Context, tenantID, createdBy int64, req CreateLabRequestRequest) (LabRequest, error) {
	normalized, err := normalizeCreateLabRequest(req)
	if err != nil {
		return LabRequest{}, err
	}
	return s.store.CreateLabRequest(ctx, tenantID, createdBy, normalized)
}

// ListLabRequests 检验工作站列表：status/date/keyword 均可为空。
func (s *Service) ListLabRequests(ctx context.Context, tenantID int64, status, date, keyword string) ([]LabRequest, error) {
	statuses, err := normalizeLabStatusFilter(status)
	if err != nil {
		return nil, err
	}
	normalizedDate, err := normalizeLabDate(date)
	if err != nil {
		return nil, err
	}
	normalizedKeyword, err := normalizeLabKeyword(keyword)
	if err != nil {
		return nil, err
	}
	return s.store.ListLabRequests(ctx, tenantID, statuses, normalizedDate, normalizedKeyword)
}

// GetLabRequest 检验申请详情（含结果项）。
func (s *Service) GetLabRequest(ctx context.Context, tenantID, id int64) (LabRequest, error) {
	if id <= 0 {
		return LabRequest{}, ErrInvalidRequest
	}
	return s.store.GetLabRequest(ctx, tenantID, id)
}

// SampleLabRequest 采样：pending → sampled。
func (s *Service) SampleLabRequest(ctx context.Context, tenantID, id int64) (LabRequest, error) {
	if id <= 0 {
		return LabRequest{}, ErrInvalidRequest
	}
	return s.store.SampleLabRequest(ctx, tenantID, id)
}

// ReportLabRequest 录入结果：sampled → reported（事务内替换结果项）。
func (s *Service) ReportLabRequest(ctx context.Context, tenantID, id int64, items []LabResultItemInput) (LabRequest, error) {
	if id <= 0 {
		return LabRequest{}, ErrInvalidRequest
	}
	normalized, err := normalizeLabResultItems(items)
	if err != nil {
		return LabRequest{}, err
	}
	return s.store.ReportLabRequest(ctx, tenantID, id, normalized)
}

// CancelLabRequest 取消申请：仅 pending。
func (s *Service) CancelLabRequest(ctx context.Context, tenantID, id int64) (LabRequest, error) {
	if id <= 0 {
		return LabRequest{}, ErrInvalidRequest
	}
	return s.store.CancelLabRequest(ctx, tenantID, id)
}
