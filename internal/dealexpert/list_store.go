package dealexpert

import (
	"context"
	"fmt"
	"strings"
)

// ListEncounters 返回接待读模型。排序与筛选都基于「结果事件优先」的有效状态，
// 保证列表、详情和经营汇总看到的是同一件事。
func (s *Store) ListEncounters(ctx context.Context, tenantID int64, params EncounterListParams, employeeFilter *int64) ([]*EncounterListItem, int, error) {
	if s == nil || s.pool == nil {
		return nil, 0, fmt.Errorf("deal expert store is nil")
	}

	args := []any{tenantID}
	conditions := "WHERE r.tenant_id = $1 AND r.deleted_at IS NULL AND lower(coalesce(r.business_scope, '')) = 'consultant'"

	if employeeFilter != nil {
		args = append(args, *employeeFilter)
		conditions += fmt.Sprintf(" AND r.employee_id = $%d", len(args))
	}
	if keyword := strings.TrimSpace(params.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		conditions += fmt.Sprintf(` AND (COALESCE(c.name, '') ILIKE $%d OR COALESCE(e.full_name, e.name, '') ILIKE $%d OR r.analysis_result::text ILIKE $%d)`, len(args), len(args), len(args))
	}
	if params.DateFrom != nil {
		args = append(args, *params.DateFrom)
		conditions += fmt.Sprintf(" AND r.recorded_at >= $%d", len(args))
	}
	if params.DateTo != nil {
		args = append(args, *params.DateTo)
		conditions += fmt.Sprintf(" AND r.recorded_at < $%d", len(args))
	}
	args = append(args, activeStatuses())
	activeArg := len(args)

	base := fmt.Sprintf(`
		WITH latest_outcome AS (
			SELECT DISTINCT ON (tenant_id, encounter_id) tenant_id, encounter_id, type
			FROM deal_outcome_events
			ORDER BY tenant_id, encounter_id, occurred_at DESC, id DESC
		),
		outcome_sum AS (
			SELECT tenant_id, encounter_id,
			       COALESCE(SUM(CASE WHEN type IN ('付款','定金') THEN amount WHEN type = '退款' THEN -amount ELSE 0 END), 0)::float8 AS net,
			       COUNT(*) AS cnt
			FROM deal_outcome_events
			GROUP BY tenant_id, encounter_id
		),
		latest_follow AS (
			SELECT DISTINCT ON (tenant_id, encounter_id) tenant_id, encounter_id, status,
			       (status = ANY($%d)) AS is_active
			FROM deal_follow_ups
			ORDER BY tenant_id, encounter_id, id DESC
		)
		SELECT
			r.id, r.tenant_id, COALESCE(r.encounter_id, 0) AS encounter_id, COALESCE(r.customer_id, 0) AS customer_id,
			COALESCE(NULLIF(c.name, ''), '未识别客户') AS customer_name,
			COALESCE(r.employee_id, 0) AS employee_id,
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未识别咨询师') AS employee_name,
			r.recorded_at,
			COALESCE(r.confirmed_deal_status, '') AS base_status,
			CASE lo.type
				WHEN '付款' THEN '成交了'
				WHEN '退款' THEN '成交了'
				WHEN '放弃' THEN '没成交'
				WHEN '暂缓' THEN '没成交'
				WHEN '定金' THEN '还在跟进'
				WHEN '预约' THEN '还在跟进'
				ELSE COALESCE(NULLIF(r.confirmed_deal_status, ''), '未知')
			END AS effective_status,
			r.converted_amount::float8 AS converted_amount,
			r.refund_amount::float8 AS refund_amount,
			CASE WHEN os.cnt > 0 THEN os.net ELSE NULL END AS effective_net,
			COALESCE(lo.type, '') AS latest_outcome_type,
			COALESCE(lf.status, '') AS follow_up_status,
			COALESCE(lf.is_active, false) AS follow_up_active,
			COALESCE(NULLIF(r.analysis_result->>'business_title', ''), latest_analysis.result->'product_findings'->0->>'product_name', '') AS product_name,
			COALESCE(latest_analysis.result->'decision'->'concerns'->0->>'type', '') AS concern_type,
			COALESCE(latest_analysis.result->'primary_action'->>'action', '') AS next_action,
			r.analysis_result
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN latest_outcome lo ON lo.tenant_id = r.tenant_id AND lo.encounter_id = r.encounter_id
		LEFT JOIN outcome_sum os ON os.tenant_id = r.tenant_id AND os.encounter_id = r.encounter_id
		LEFT JOIN latest_follow lf ON lf.tenant_id = r.tenant_id AND lf.encounter_id = r.encounter_id
		LEFT JOIN LATERAL (
			SELECT result FROM deal_expert_analyses a
			WHERE a.tenant_id = r.tenant_id AND a.recording_id = r.id AND a.status = 'analyzed'
			ORDER BY a.id DESC LIMIT 1
		) latest_analysis ON true
		%s`, activeArg, conditions)

	outer := make([]string, 0, 4)
	if status := strings.TrimSpace(params.Status); status != "" {
		args = append(args, status)
		outer = append(outer, fmt.Sprintf("t.effective_status = $%d", len(args)))
	}
	switch params.Decision {
	case "paid":
		outer = append(outer, "t.effective_status = '成交了'")
	case "appointed":
		outer = append(outer, "t.latest_outcome_type = '预约'")
	case "deposit":
		outer = append(outer, "t.latest_outcome_type = '定金'")
	case "postponed":
		outer = append(outer, "t.latest_outcome_type = '暂缓'")
	case "abandoned":
		outer = append(outer, "(t.latest_outcome_type = '放弃' OR (t.latest_outcome_type = '' AND t.effective_status = '没成交'))")
	case "high_intent":
		outer = append(outer, "t.effective_status = '还在跟进' AND t.latest_outcome_type NOT IN ('预约','定金')")
	}
	if params.CustomerLinked != nil {
		if *params.CustomerLinked {
			outer = append(outer, "t.customer_id IS NOT NULL")
		} else {
			outer = append(outer, "t.customer_id IS NULL")
		}
	}
	switch params.FollowUpState {
	case "none":
		outer = append(outer, "t.follow_up_status = ''")
	case "active":
		outer = append(outer, "t.follow_up_active")
	case "completed":
		outer = append(outer, "t.follow_up_status = '已完成'")
	case "paused":
		outer = append(outer, "t.follow_up_status = '暂停联系'")
	}
	where := "1=1"
	if len(outer) > 0 {
		where = strings.Join(outer, " AND ")
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM (%s) t WHERE %s", base, where)
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count deal encounters: %w", err)
	}

	args = append(args, params.PageSize, (params.Page-1)*params.PageSize)
	query := fmt.Sprintf(`
		SELECT t.id, t.tenant_id, t.encounter_id, t.customer_id, t.customer_name, t.employee_id,
		       t.employee_name, t.recorded_at, t.base_status, t.effective_status,
		       t.converted_amount, t.refund_amount, t.effective_net,
		       t.latest_outcome_type, t.follow_up_status, t.product_name, t.concern_type, t.next_action, t.analysis_result
		FROM (%s) t
		WHERE %s
		ORDER BY t.recorded_at DESC NULLS LAST, t.id DESC
		LIMIT $%d OFFSET $%d`, base, where, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list deal encounters: %w", err)
	}
	defer rows.Close()

	items := make([]*EncounterListItem, 0, params.PageSize)
	for rows.Next() {
		item := &EncounterListItem{}
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.EncounterID, &item.CustomerID, &item.CustomerName,
			&item.EmployeeID, &item.EmployeeName, &item.RecordedAt, &item.BaseDealStatus,
			&item.EffectiveDealStatus, &item.ConvertedAmount, &item.RefundAmount, &item.EffectiveNetAmount,
			&item.LatestOutcomeType, &item.FollowUpStatus, &item.ProductName, &item.ConcernType, &item.NextAction, &item.AnalysisResult,
		); err != nil {
			return nil, 0, fmt.Errorf("scan deal encounter: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
