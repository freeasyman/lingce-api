package dealexpert

import (
	"context"
	"fmt"
	"time"
)

// Overview 汇总一个租户在时间范围内的经营事实。
// 口径：
//   - 到院、成交状态来自 recordings；
//   - 金额优先来自 deal_outcome_events（预约/定金/付款/退款），
//     没有结果事件时才退回 recordings 的人工确认金额，并写明来源；
//   - 跟进完成度来自 deal_follow_ups，不进入个人排名。
func (s *Store) Overview(ctx context.Context, tenantID int64, dateFrom, dateTo *time.Time, employeeFilter *int64) (*Overview, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("deal expert store is nil")
	}
	out := &Overview{AmountSource: "none"}
	if dateFrom != nil {
		out.DateFrom = *dateFrom
	}
	if dateTo != nil {
		out.DateTo = *dateTo
	}

	err := s.pool.QueryRow(ctx, `
		WITH scope AS (
			SELECT id, confirmed_deal_status,
			       COALESCE(converted_amount, 0)::float8 AS converted,
			       COALESCE(refund_amount, 0)::float8 AS refunded,
			       analysis_result
			FROM recordings
			WHERE tenant_id = $1
			  AND deleted_at IS NULL
			  AND lower(COALESCE(business_scope, '')) = 'consultant'
			  AND ($2::timestamptz IS NULL OR recorded_at >= $2)
			  AND ($3::timestamptz IS NULL OR recorded_at < $3)
			  AND ($4::bigint IS NULL OR employee_id = $4)
		)
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE confirmed_deal_status = '成交了'),
			COUNT(*) FILTER (WHERE confirmed_deal_status = '还在跟进'),
			COUNT(*) FILTER (WHERE confirmed_deal_status = '没成交'),
			COUNT(*) FILTER (WHERE confirmed_deal_status IS NULL),
			COUNT(*) FILTER (WHERE confirmed_deal_status IN ('还在跟进', '没成交')),
			COALESCE(SUM(converted) FILTER (WHERE confirmed_deal_status = '成交了'), 0),
			COALESCE(SUM(refunded) FILTER (WHERE confirmed_deal_status = '成交了'), 0),
			COUNT(*) FILTER (WHERE COALESCE(analysis_result->'persuasive'->'recommend_check'->>'missed', '') <> '')
		FROM scope
	`, tenantID, dateFrom, dateTo, employeeFilter).Scan(
		&out.TotalEncounters, &out.Converted, &out.Following, &out.NotConverted, &out.Unknown,
		&out.OpportunityCount, &out.GrossAmount, &out.RefundAmount, &out.ProductMissed,
	)
	if err != nil {
		return nil, fmt.Errorf("overview recordings: %w", err)
	}

	eventNet := 0.0
	eventGross := 0.0
	eventRefund := 0.0
	eventCount := 0
	rows, err := s.pool.Query(ctx, `
		SELECT type, COUNT(*), COALESCE(SUM(amount), 0)::float8
		FROM deal_outcome_events
		WHERE tenant_id = $1
		  AND ($2::timestamptz IS NULL OR occurred_at >= $2)
		  AND ($3::timestamptz IS NULL OR occurred_at < $3)
		GROUP BY type
	`, tenantID, dateFrom, dateTo)
	if err != nil {
		return nil, fmt.Errorf("overview outcomes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var eventType string
		var count int
		var amount float64
		if err := rows.Scan(&eventType, &count, &amount); err != nil {
			return nil, fmt.Errorf("scan outcome summary: %w", err)
		}
		eventCount += count
		switch eventType {
		case "预约":
			out.AppointmentCount = count
		case "定金":
			out.DepositCount = count
			eventGross += amount
		case "付款":
			out.PaymentCount = count
			eventGross += amount
		case "退款":
			out.RefundCount = count
			eventRefund += amount
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if eventCount > 0 {
		eventNet = eventGross - eventRefund
		out.AmountSource = "outcome_events"
		out.GrossAmount = eventGross
		out.RefundAmount = eventRefund
		out.NetAmount = eventNet
	} else {
		out.AmountSource = "recordings"
		out.NetAmount = out.GrossAmount - out.RefundAmount
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = '待处理'),
			COUNT(*) FILTER (WHERE status IN ('待处理','处理中','等待客户','等待内部协作','暂停联系') AND due_at IS NOT NULL AND due_at < NOW()),
			COUNT(*) FILTER (WHERE status = '已完成')
		FROM deal_follow_ups
		WHERE tenant_id = $1
		  AND ($2::bigint IS NULL OR owner_employee_id = $2)
	`, tenantID, employeeFilter).Scan(&out.FollowUpPending, &out.FollowUpOverdue, &out.FollowUpCompleted); err != nil {
		return nil, fmt.Errorf("overview follow ups: %w", err)
	}

	return out, nil
}
