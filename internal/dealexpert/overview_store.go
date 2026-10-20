package dealexpert

import (
	"context"
	"fmt"
	"time"
)

// Overview 汇总一个租户在时间范围内的经营事实。
// 口径：
//   - 到院、有效成交状态来自 recordings + 结果事件；
//   - 金额优先来自 deal_outcome_events，没有结果事件时才退回 recordings 的人工确认金额，并写明来源；
//   - 跟进完成度与数据缺口来自 deal_follow_ups，不进入个人排名。
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

	active := activeStatuses()
	err := s.pool.QueryRow(ctx, `
		WITH scope AS (
			SELECT r.id, r.encounter_id, r.customer_id,
			       CASE lo.type
			         WHEN '付款' THEN '成交了'
			         WHEN '退款' THEN '成交了'
			         WHEN '放弃' THEN '没成交'
			         WHEN '暂缓' THEN '没成交'
			         WHEN '定金' THEN '还在跟进'
			         WHEN '预约' THEN '还在跟进'
			         ELSE COALESCE(NULLIF(r.confirmed_deal_status, ''), '未知')
			       END AS effective_status,
			       COALESCE(r.converted_amount, 0)::float8 AS converted,
			       COALESCE(r.refund_amount, 0)::float8 AS refunded,
			       COALESCE(os.cnt, 0) AS outcome_count,
			       COALESCE(af.status, '') AS follow_status,
			       r.analysis_result
			FROM recordings r
			LEFT JOIN LATERAL (
				SELECT type FROM deal_outcome_events o
				WHERE o.tenant_id = r.tenant_id AND o.encounter_id = r.encounter_id
				ORDER BY occurred_at DESC, id DESC LIMIT 1
			) lo ON TRUE
			LEFT JOIN LATERAL (
				SELECT COUNT(*) AS cnt FROM deal_outcome_events o
				WHERE o.tenant_id = r.tenant_id AND o.encounter_id = r.encounter_id
			) os ON TRUE
			LEFT JOIN LATERAL (
				SELECT status FROM deal_follow_ups f
				WHERE f.tenant_id = r.tenant_id AND f.encounter_id = r.encounter_id AND f.status = ANY($5)
				ORDER BY f.id DESC LIMIT 1
			) af ON TRUE
			WHERE r.tenant_id = $1
			  AND r.deleted_at IS NULL
			  AND lower(COALESCE(r.business_scope, '')) = 'consultant'
			  AND ($2::timestamptz IS NULL OR r.recorded_at >= $2)
			  AND ($3::timestamptz IS NULL OR r.recorded_at < $3)
			  AND ($4::bigint IS NULL OR r.employee_id = $4)
		)
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE effective_status = '成交了'),
			COUNT(*) FILTER (WHERE effective_status = '还在跟进'),
			COUNT(*) FILTER (WHERE effective_status = '没成交'),
			COUNT(*) FILTER (WHERE effective_status = '未知'),
			COUNT(*) FILTER (WHERE effective_status IN ('还在跟进', '没成交')),
			COALESCE(SUM(converted) FILTER (WHERE effective_status = '成交了'), 0),
			COALESCE(SUM(refunded) FILTER (WHERE effective_status = '成交了'), 0),
			COUNT(*) FILTER (WHERE COALESCE(analysis_result->'persuasive'->'recommend_check'->>'missed', '') <> ''),
			COUNT(*) FILTER (WHERE encounter_id IS NULL),
			COUNT(*) FILTER (WHERE customer_id IS NULL),
			COUNT(*) FILTER (WHERE effective_status = '成交了' AND outcome_count = 0 AND converted = 0),
			COUNT(*) FILTER (WHERE effective_status = '还在跟进' AND follow_status = '')
		FROM scope
	`, tenantID, dateFrom, dateTo, employeeFilter, active).Scan(
		&out.TotalEncounters, &out.Converted, &out.Following, &out.NotConverted, &out.Unknown,
		&out.OpportunityCount, &out.GrossAmount, &out.RefundAmount, &out.ProductMissed,
		&out.UnlinkedEncounters, &out.IdentityPending, &out.AmountPending, &out.FollowUpMissing,
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
			COUNT(*) FILTER (WHERE status = ANY($2) AND due_at IS NOT NULL AND due_at < NOW()),
			COUNT(*) FILTER (WHERE status = '已完成')
		FROM deal_follow_ups
		WHERE tenant_id = $1
		  AND ($3::bigint IS NULL OR owner_employee_id = $3)
	`, tenantID, active, employeeFilter).Scan(&out.FollowUpPending, &out.FollowUpOverdue, &out.FollowUpCompleted); err != nil {
		return nil, fmt.Errorf("overview follow ups: %w", err)
	}

	return out, nil
}
