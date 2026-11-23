package emrtemplate

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ResolvedTemplate 是统一匹配的结果。
type ResolvedTemplate struct {
	ID           string
	Code         string
	Scope        string
	DocumentType string
	VisitType    string
}

// ResolveCurrentTemplate 是全系统唯一的模板解析入口：
// 租户当前 → 平台当前 → 标准门诊兜底。
// departmentCode 必须是医生的 medical_department_code，同时也是模板 code。
func ResolveCurrentTemplate(ctx context.Context, pool *pgxpool.Pool, tenantID int64, departmentCode, documentType, visitType string) (*ResolvedTemplate, error) {
	if pool == nil {
		return nil, fmt.Errorf("模板解析未配置数据库连接")
	}
	departmentCode = strings.TrimSpace(departmentCode)
	if departmentCode == "" {
		return nil, fmt.Errorf("医生没有配置医疗科室，无法匹配病历模板")
	}
	steps := []struct {
		tenantID int64
		code     string
		scope    string
	}{
		{tenantID, departmentCode, "tenant"},
		{0, departmentCode, "platform"},
		{0, "standard_outpatient", "platform"},
	}
	for _, step := range steps {
		resolved, err := findCurrentTemplate(ctx, pool, step.tenantID, step.code, documentType, visitType)
		if err != nil {
			return nil, err
		}
		if resolved != nil {
			resolved.Scope = step.scope
			return resolved, nil
		}
	}
	return nil, fmt.Errorf("没有可用的当前病历模板：科室=%s", departmentCode)
}

func findCurrentTemplate(ctx context.Context, pool *pgxpool.Pool, tenantID int64, code, documentType, visitType string) (*ResolvedTemplate, error) {
	args := []any{tenantID, strings.TrimSpace(code)}
	where := "COALESCE(tenant_id, 0) = $1 AND code = $2 AND status = 'published' AND is_current = true"
	if value := strings.TrimSpace(documentType); value != "" {
		args = append(args, value)
		where += fmt.Sprintf(" AND document_type = $%d", len(args))
	}
	if value := strings.TrimSpace(visitType); value != "" {
		args = append(args, value)
		where += fmt.Sprintf(" AND visit_type IN ($%d, '通用')", len(args))
	}
	var item ResolvedTemplate
	err := pool.QueryRow(ctx, "SELECT id, code, scope, document_type, visit_type FROM emr_templates WHERE "+where+" ORDER BY updated_at DESC, id DESC LIMIT 1", args...).
		Scan(&item.ID, &item.Code, &item.Scope, &item.DocumentType, &item.VisitType)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询当前病历模板失败：%w", err)
	}
	return &item, nil
}
