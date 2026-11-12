package billing

// 机构档案 / 管理员的变更日志：写仓库现有的 system_action_logs。
//
// 约定：
//   - actor 是当前登录的运营账号（operations_admins.id）；
//   - target 是被改的机构（object_type=institution）或机构管理员（object_type=institution_admin，
//     object_id 是 employees.id），两者不混用；
//   - 日志失败不阻塞业务（best-effort），只记 slog.Warn。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// institutionActionLog 是一次机构/管理员变更的日志入参。
type institutionActionLog struct {
	TenantID   int64
	TenantName string
	ActorID    int64
	ActorName  string
	LogType    string
	ActionCode string
	ActionName string
	ObjectType string
	ObjectID   string
	ObjectName string
	Before     map[string]any
	After      map[string]any
}

// InsertSystemActionLog 写一条 system_action_logs（before/after 以 JSONB 存）。
func (s *Store) InsertSystemActionLog(ctx context.Context, input institutionActionLog) error {
	if input.ActionCode == "" {
		return nil
	}
	// tenant_id 允许为空：价目改版等平台级操作不属于任何机构。
	var tenantID any
	if input.TenantID > 0 {
		tenantID = input.TenantID
	}
	before, err := marshalSummary(input.Before)
	if err != nil {
		return err
	}
	after, err := marshalSummary(input.After)
	if err != nil {
		return err
	}
	logType := input.LogType
	if logType == "" {
		logType = "institution"
	}
	actionName := input.ActionName
	if actionName == "" {
		actionName = input.ActionCode
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO system_action_logs (
    tenant_id, tenant_name, actor_id, actor_name,
    log_type, action_code, action_name,
    route_path, result, object_type, object_id, object_name,
    before_summary, after_summary, created_at
)
VALUES (
    $1, $2, $3, $4,
    $5, $6, $7,
    '', 'success', $8, $9, $10,
    $11::jsonb, $12::jsonb, NOW()
)`,
		tenantID, nullableString(input.TenantName), input.ActorID, nullableString(input.ActorName),
		logType, input.ActionCode, actionName,
		nullableString(input.ObjectType), nullableString(input.ObjectID), nullableString(input.ObjectName),
		string(before), string(after))
	if err != nil {
		return fmt.Errorf("insert system action log: %w", err)
	}
	return nil
}

// marshalSummary 把变更摘要序列化成 JSON 文本；nil 落成空对象。
func marshalSummary(summary map[string]any) ([]byte, error) {
	if len(summary) == 0 {
		return []byte(`{}`), nil
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		return nil, fmt.Errorf("marshal action log summary: %w", err)
	}
	return raw, nil
}

// GetOperationsAdminName 取运营账号展示名（写日志用）；查不到返回空串。
func (s *Store) GetOperationsAdminName(ctx context.Context, adminID int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
SELECT COALESCE(NULLIF(name, ''), NULLIF(username, ''), COALESCE(phone, ''))
FROM operations_admins
WHERE id = $1`, adminID).Scan(&name)
	if err != nil {
		// 找不到账号不该阻塞写操作；日志里 actor_name 留空即可。
		return "", nil
	}
	return name, nil
}

// nullableString 把空串转成 SQL NULL（日志列允许为空）。
func nullableString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// InstitutionActionLog 是机构档案「变更记录」的一行（system_action_logs 的读模型）。
type InstitutionActionLog struct {
	ID         int64          `json:"id"`
	At         string         `json:"at"`
	Actor      string         `json:"actor"`
	ActionCode string         `json:"actionCode"`
	ActionName string         `json:"actionName"`
	LogType    string         `json:"logType"`
	ObjectType string         `json:"objectType"`
	ObjectID   string         `json:"objectId"`
	ObjectName string         `json:"objectName"`
	Result     string         `json:"result"`
	Detail     string         `json:"detail"`
	Before     map[string]any `json:"before"`
	After      map[string]any `json:"after"`
}

// ListSystemActionLogs 按机构查最近 limit 条变更记录（新→旧）。
func (s *Store) ListSystemActionLogs(ctx context.Context, tenantID int64, limit int) ([]InstitutionActionLog, error) {
	if tenantID <= 0 {
		return []InstitutionActionLog{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
SELECT id,
       to_char(created_at, 'YYYY-MM-DD HH24:MI'),
       COALESCE(NULLIF(actor_name, ''), '系统'),
       action_code, action_name, log_type,
       COALESCE(object_type, ''), COALESCE(object_id, ''), COALESCE(object_name, ''),
       COALESCE(result, 'success'),
       COALESCE(before_summary, '{}'::jsonb)::text,
       COALESCE(after_summary, '{}'::jsonb)::text
FROM system_action_logs
WHERE tenant_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list system action logs: %w", err)
	}
	defer rows.Close()

	items := make([]InstitutionActionLog, 0, 32)
	for rows.Next() {
		var (
			item       InstitutionActionLog
			beforeJSON string
			afterJSON  string
		)
		if err := rows.Scan(&item.ID, &item.At, &item.Actor, &item.ActionCode, &item.ActionName, &item.LogType,
			&item.ObjectType, &item.ObjectID, &item.ObjectName, &item.Result, &beforeJSON, &afterJSON); err != nil {
			return nil, fmt.Errorf("scan system action log: %w", err)
		}
		item.Before = decodeLogSummary(beforeJSON)
		item.After = decodeLogSummary(afterJSON)
		item.Detail = actionLogDetail(item)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate system action logs: %w", err)
	}
	return items, nil
}

// decodeLogSummary 把 JSONB 摘要解码成 map；坏数据不阻塞列表。
func decodeLogSummary(raw string) map[string]any {
	summary := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return summary
	}
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		return map[string]any{}
	}
	return summary
}

// actionStatusLabel 把变更记录里的状态码转成中文（历史日志展示用）。
func actionStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "created":
		return "待收款"
	case "paid":
		return "部分收款"
	case "settled":
		return "已结清"
	case "refunded":
		return "已退款"
	case "cancelled":
		return "已取消"
	case "void", "voided":
		return "已作废"
	case "applied":
		return "待开票"
	case "issued":
		return "已开票"
	case "unallocated":
		return "未核销"
	case "partial":
		return "部分核销"
	case "allocated":
		return "已核销"
	default:
		return status
	}
}

// actionLogDetail 给变更记录生成一行可读说明：优先原因，其次按动作拼装金额/发票号等关键字段。
func actionLogDetail(item InstitutionActionLog) string {
	// 开票动作有稳定的字段组合，直接拼成财务能读懂的一句话（历史日志也能受益）。
	if item.ActionCode == "billing.invoice.create" {
		amount := strings.TrimSpace(fmt.Sprint(item.After["amount"]))
		invoiceType := strings.TrimSpace(fmt.Sprint(item.After["invoiceType"]))
		if amount != "" && amount != "<nil>" {
			return fmt.Sprintf("开票金额 %s · %s", amount, invoiceType)
		}
	}
	if item.ActionCode == "billing.invoice.issue" {
		invoiceNo := strings.TrimSpace(fmt.Sprint(item.After["invoiceNo"]))
		issuedOn := strings.TrimSpace(fmt.Sprint(item.After["issuedOn"]))
		if invoiceNo != "" && invoiceNo != "<nil>" {
			return fmt.Sprintf("发票号 %s · 开票日期 %s", invoiceNo, issuedOn)
		}
	}
	for _, key := range []string{"reason", "note", "detail"} {
		if value, ok := item.After[key]; ok {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
				return text
			}
		}
	}
	if value, ok := item.After["status"]; ok {
		return "状态：" + actionStatusLabel(fmt.Sprint(value))
	}
	if len(item.After) == 0 {
		return ""
	}
	keys := make([]string, 0, len(item.After))
	for key := range item.After {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return "变更字段：" + strings.Join(keys, "、")
}
