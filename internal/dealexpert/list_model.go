package dealexpert

import (
	"context"
	"time"
)

// EncounterListItem 是赢客专家的接待读模型：
// 状态与金额以 result events 为准（只追加、不覆盖），没有结果事件时才回落到录音上的人工确认值。
type EncounterListItem struct {
	ID                  int64          `json:"id"`
	TenantID            int64          `json:"tenant_id"`
	EncounterID         int64          `json:"encounter_id"`
	CustomerID          *int64         `json:"customer_id,omitempty"`
	CustomerName        string         `json:"customer_name"`
	EmployeeID          int64          `json:"employee_id"`
	EmployeeName        string         `json:"employee_name"`
	RecordedAt          *time.Time     `json:"recorded_at,omitempty"`
	BaseDealStatus      string         `json:"confirmed_deal_status,omitempty"`
	EffectiveDealStatus string         `json:"effective_deal_status"`
	ConvertedAmount     *float64       `json:"converted_amount,omitempty"`
	RefundAmount        *float64       `json:"refund_amount,omitempty"`
	EffectiveNetAmount  *float64       `json:"effective_net_amount,omitempty"`
	LatestOutcomeType   string         `json:"latest_outcome_type,omitempty"`
	FollowUpStatus      string         `json:"follow_up_status,omitempty"`
	AnalysisResult      map[string]any `json:"analysis_result,omitempty"`
}

type EncounterListParams struct {
	Page     int
	PageSize int
	Status   string
	Keyword  string
	DateFrom *time.Time
	DateTo   *time.Time
	// CustomerLinked: nil 不筛选；true 只看已关联客户；false 只看未关联客户。
	CustomerLinked *bool
	// FollowUpState: "" 不筛选；none / active / completed / paused。
	FollowUpState string
	// Decision: "" 不筛选；paid / appointed / deposit / postponed / abandoned / high_intent。
	Decision string
}

func (s *Service) ListEncounters(ctx context.Context, tenantID int64, params EncounterListParams, employeeFilter *int64) ([]*EncounterListItem, int, error) {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 || params.PageSize > 100 {
		params.PageSize = 20
	}
	switch params.Status {
	case "", "成交了", "没成交", "还在跟进", "未知":
	default:
		return nil, 0, ErrInvalidOutcomeStatus
	}
	switch params.FollowUpState {
	case "", "none", "active", "completed", "paused":
	default:
		return nil, 0, ErrInvalidFollowUpState
	}
	switch params.Decision {
	case "", "paid", "appointed", "deposit", "postponed", "abandoned", "high_intent":
	default:
		return nil, 0, ErrInvalidDecisionFilter
	}
	return s.store.ListEncounters(ctx, tenantID, params, employeeFilter)
}
