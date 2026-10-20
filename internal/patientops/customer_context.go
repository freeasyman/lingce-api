package patientops

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// loadCustomerContext 只给模型最小、可核对的客户连续性信息，不把完整档案或推断塞进提示词。
func (s *Service) loadCustomerContext(ctx context.Context, tenantID, customerID int64) string {
	if customerID <= 0 {
		return "未绑定客户；按孤立截图理解，不要假设客户身份、关系或历史沟通。"
	}
	var name, status, source, firstChannel, lastItem, lastDeal, notes string
	var lastContacted *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(c.name,''), COALESCE(c.status,''), COALESCE(c.source,''), COALESCE(c.first_channel,''),
		       COALESCE((
		           SELECT NULLIF(TRIM(COALESCE(
		               r.converted_item,
		               r.analysis_result->'consultation_record'->>'consultation_item',
		               r.analysis_result->'consultation_record'->>'treatment_item',
		               r.analysis_result->'analysis_summary'->'visit_outcome'->>'treatment_item',
		               r.analysis_result->'raw'->'consultant_conversion_analysis_v1'->'persuasive'->>'consultation_item',
		               r.analysis_result->'raw'->'consultant_conversion_analysis_v1'->'persuasive'->>'treatment_item',
		               ''
		           )), '')
		           FROM recordings r
		           WHERE r.tenant_id=c.tenant_id AND r.customer_id=c.id
		           ORDER BY COALESCE(r.recorded_at, r.created_at) DESC, r.id DESC
		           LIMIT 1
		       ), ''),
		       COALESCE((
		           SELECT NULLIF(TRIM(COALESCE(
		               r.confirmed_deal_status,
		               r.analysis_result->'analysis_summary'->'visit_outcome'->>'status',
		               r.analysis_result->'deal_outcome'->>'result',
		               r.analysis_result->'decision_state'->>'status',
		               r.analysis_result->'raw'->'consultant_conversion_analysis_v1'->'persuasive'->>'deal_status',
		               ''
		           )), '')
		           FROM recordings r
		           WHERE r.tenant_id=c.tenant_id AND r.customer_id=c.id
		           ORDER BY COALESCE(r.recorded_at, r.created_at) DESC, r.id DESC
		           LIMIT 1
		       ), ''),
		       COALESCE(c.notes,''), c.last_contacted_at
		FROM customers c
		WHERE c.tenant_id=$1 AND c.id=$2 AND c.deleted_at IS NULL
	`, tenantID, customerID).Scan(&name, &status, &source, &firstChannel, &lastItem, &lastDeal, &notes, &lastContacted)
	if err != nil {
		slog.Warn("load screenshot customer context failed", "tenant_id", tenantID, "customer_id", customerID, "error", err)
		return "客户上下文读取失败；本次按孤立截图理解，不要假设客户身份。"
	}
	parts := []string{"客户身份（已确认）：" + firstNonEmpty(name, "未命名")}
	history := make([]string, 0, 6)
	if status != "" {
		history = append(history, "状态："+status)
	}
	if channel := firstNonEmpty(firstChannel, source); channel != "" {
		history = append(history, "来源："+channel)
	}
	if lastContacted != nil {
		history = append(history, "最近联系："+lastContacted.Format("2006-01-02 15:04"))
	}
	if lastItem != "" {
		history = append(history, "最近咨询项目："+lastItem)
	}
	if lastDeal != "" {
		history = append(history, "最近成交结果："+lastDeal)
	}
	if strings.TrimSpace(notes) != "" {
		history = append(history, "客户备注（可能是员工记录或推测）："+truncatePlanText(strings.TrimSpace(notes), 300))
	}
	if len(history) > 0 {
		parts = append(parts, "数据库历史（需核对，不代表客户当前表达）："+strings.Join(history, "；"))
	}
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(i.interaction_type,''), '沟通'),
		       COALESCE(NULLIF(i.direction,''), '未标记'),
		       COALESCE(NULLIF(i.content_summary,''), NULLIF(i.raw_content,''), ''),
		       COALESCE(i.interacted_at, i.occurred_at, i.created_at)
		FROM customer_interactions i
		JOIN customers c ON c.id=i.customer_id
		WHERE c.tenant_id=$1 AND i.customer_id=$2 AND c.deleted_at IS NULL
		ORDER BY COALESCE(i.interacted_at, i.occurred_at, i.created_at) DESC
		LIMIT 3
	`, tenantID, customerID)
	if err != nil {
		slog.Warn("load customer interaction context failed", "tenant_id", tenantID, "customer_id", customerID, "error", err)
	} else {
		defer rows.Close()
		var interactions []string
		for rows.Next() {
			var typ, direction, content string
			var at time.Time
			if scanErr := rows.Scan(&typ, &direction, &content, &at); scanErr != nil {
				slog.Warn("scan customer interaction context failed", "tenant_id", tenantID, "customer_id", customerID, "error", scanErr)
				continue
			}
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}
			interactions = append(interactions, fmt.Sprintf("%s %s/%s：%s", at.Format("2006-01-02"), typ, direction, truncatePlanText(content, 180)))
		}
		if len(interactions) > 0 {
			parts = append(parts, "最近沟通（数据库记录，需核对，不等同于客户当前表达）："+strings.Join(interactions, "；"))
		}
	}
	return strings.Join(parts, "；") + "。身份字段可用于确认对象；备注和历史沟通只是线索，必须核对，不能当作客户当前表达或已确认事实。"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
