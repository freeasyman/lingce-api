// Package readmark 提供通用的「已读」记录：菜单角标只统计当前用户没看过的待办。
// 各业务域在自己的计数 SQL 里用 NOT EXISTS(user_resource_reads ...) 剥掉已读项。
package readmark

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 资源类型白名单：与前端各产品的角标一一对应。
const (
	TypeEMRRecord         = "emr_record"
	TypeFollowupTask      = "followup_task"
	TypeContentInsight    = "content_insight"
	TypeComplianceFinding = "compliance_finding"
	TypeCompliancePerson  = "compliance_person"
	TypePatientScreenshot = "patient_screenshot"
	TypePatientTask       = "patient_task"
)

var allowedTypes = map[string]bool{
	TypeEMRRecord:         true,
	TypeFollowupTask:      true,
	TypeContentInsight:    true,
	TypeComplianceFinding: true,
	TypeCompliancePerson:  true,
	TypePatientScreenshot: true,
	TypePatientTask:       true,
}

// ValidType 判断资源类型是否在白名单内。
func ValidType(resourceType string) bool {
	return allowedTypes[strings.TrimSpace(resourceType)]
}

// MarkRead 幂等写入已读；同一条记录重复标记不报错。
func MarkRead(ctx context.Context, q *pgxpool.Pool, tenantID, employeeID int64, resourceType string, ids []string) error {
	if tenantID <= 0 || employeeID <= 0 {
		return fmt.Errorf("tenant and employee required")
	}
	if !ValidType(resourceType) {
		return fmt.Errorf("invalid resource type")
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := q.Exec(ctx, `
			INSERT INTO user_resource_reads (tenant_id, employee_id, resource_type, resource_id)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (tenant_id, employee_id, resource_type, resource_id) DO NOTHING
		`, tenantID, employeeID, resourceType, id); err != nil {
			return fmt.Errorf("mark read: %w", err)
		}
	}
	return nil
}
