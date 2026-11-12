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
	if input.TenantID <= 0 || input.ActionCode == "" {
		return nil
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
		input.TenantID, nullableString(input.TenantName), input.ActorID, nullableString(input.ActorName),
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
