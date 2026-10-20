package dealexpert

import (
	"context"
	"fmt"
	"time"
)

// Overview 是院长视角的经营汇总。金额只来自业务事实：
// 结果事件优先；没有结果事件时退回录音上的人工确认金额，并标明来源。
type Overview struct {
	DateFrom time.Time `json:"date_from,omitempty"`
	DateTo   time.Time `json:"date_to,omitempty"`

	TotalEncounters int `json:"total_encounters"`
	Converted       int `json:"converted"`
	Following       int `json:"following"`
	NotConverted    int `json:"not_converted"`
	Unknown         int `json:"unknown"`

	GrossAmount  float64 `json:"gross_amount"`
	RefundAmount float64 `json:"refund_amount"`
	NetAmount    float64 `json:"net_amount"`
	AmountSource string  `json:"amount_source"`

	AppointmentCount int `json:"appointment_count"`
	DepositCount     int `json:"deposit_count"`
	PaymentCount     int `json:"payment_count"`
	RefundCount      int `json:"refund_count"`

	OpportunityCount int `json:"opportunity_count"`
	ProductMissed    int `json:"product_missed"`

	FollowUpPending   int `json:"follow_up_pending"`
	FollowUpOverdue   int `json:"follow_up_overdue"`
	FollowUpCompleted int `json:"follow_up_completed"`
}

func (s *Service) Overview(ctx context.Context, tenantID int64, dateFrom, dateTo *time.Time, employeeFilter *int64) (*Overview, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	return s.store.Overview(ctx, tenantID, dateFrom, dateTo, employeeFilter)
}
