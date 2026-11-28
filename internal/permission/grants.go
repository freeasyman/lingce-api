package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// 产品标识（命令与报表使用，避免各产品各写一套字符串）。
const (
	ProductEMR        = "emr"
	ProductCompliance = "compliance"
	ProductFollowup   = "followup"
	ProductPatientOps = "patient-operations"
	ProductDealExpert = "deal-expert"
	ProductContent    = "content"
)

// GrantStatus 是一次默认发放的结果。
type GrantStatus string

const (
	GrantStatusGranted    GrantStatus = "granted"
	GrantStatusExists     GrantStatus = "exists"
	GrantStatusWouldGrant GrantStatus = "would_grant"
	GrantStatusSkipped    GrantStatus = "skipped"
)

// GrantResult 描述某个产品对某个管理员的默认授权结果。
type GrantResult struct {
	Product string      `json:"product"`
	Table   string      `json:"table,omitempty"`
	Status  GrantStatus `json:"status"`
	Reason  string      `json:"reason,omitempty"`
}

// QueryExecer 是 pgxpool.Pool 与 pgx.Tx 共有的查询 + 写入接口。
type QueryExecer interface {
	Querier
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// productGrantSpec 是单个产品的默认管理员授权定义。
// 表名与角色/能力码必须和各产品权限页保存的完全一致，否则会出现「默认给了但页面不认」。
type productGrantSpec struct {
	Product   string
	Table     string
	RoleCode  string
	Abilities []string
	// SkipReason 非空表示产品没有独立权限表，跳过并记录原因。
	SkipReason string
}

var (
	emrAdminAbilities = []string{
		"record.read", "record.read_all", "record.create", "record.edit", "record.submit",
		"record.archive", "record.history.read", "record.export", "template.manage", "quality.manage",
	}
	complianceAdminAbilities = []string{
		"compliance.finding.read", "compliance.finding.review", "compliance.rule.read",
		"compliance.rule.manage", "compliance.run.read", "compliance.permission.manage",
	}
	followupAdminAbilities = []string{
		"followup.task.read", "followup.task.execute", "followup.task.assign",
		"followup.partnership.manage", "followup.scope.manage", "followup.permission.manage",
	}
	patientOpsAdminAbilities = []string{
		"patient_ops.tasks.read_all", "patient_ops.permission.manage", "patient_ops.tasks.assign",
	}
	dealExpertAdminAbilities = []string{
		"deal.consultation.read", "deal.task.read", "deal.task.execute", "deal.performance.read",
		"deal.product.read", "deal.workbench.read", "deal.ledger.read", "deal.ledger.export",
		"deal.ledger.correct", "deal.quotes.read", "deal.quotes.manage", "deal.product.manage",
		"deal.permission.manage",
	}
	contentAdminAbilities = []string{
		"content.item.read", "content.item.create", "content.item.edit", "content.item.publish",
		"content.template.manage", "content.analysis.read", "content.permission.manage",
	}
)

var productGrantSpecs = []productGrantSpec{
	{Product: ProductEMR, Table: "emr_permission_assignments", RoleCode: "emr_admin", Abilities: emrAdminAbilities},
	{Product: ProductCompliance, Table: "compliance_permission_assignments", RoleCode: "compliance_admin", Abilities: complianceAdminAbilities},
	{Product: ProductFollowup, Table: "followup_permission_assignments", RoleCode: "followup_admin", Abilities: followupAdminAbilities},
	{Product: ProductPatientOps, Table: "patient_ops_permission_assignments", RoleCode: "patient_ops_admin", Abilities: patientOpsAdminAbilities},
	{Product: ProductDealExpert, Table: "deal_expert_permission_assignments", RoleCode: "deal_admin", Abilities: dealExpertAdminAbilities},
	{Product: ProductContent, Table: "content_permission_assignments", RoleCode: "content_admin", Abilities: contentAdminAbilities},
}

// AdminAbilities 返回某产品默认管理员的完整能力包（只读副本）。
func AdminAbilities(product string) []string {
	for _, spec := range productGrantSpecs {
		if spec.Product == product {
			return slices.Clone(spec.Abilities)
		}
	}
	return nil
}

// SetProductAdmin 将某员工设为/撤销某产品的管理员。
//
// isAdmin=true 时按产品管理员口径 upsert（enabled + admin 角色 + 完整能力包）；
// isAdmin=false 时把该产品的权限行停用（enabled=false），不物理删除、保留可追溯。
// 六个产品（含获客先锋 content）各写自己的 *_permission_assignments，口径一致。
func SetProductAdmin(ctx context.Context, q QueryExecer, product string, tenantID, employeeID, actorID int64, isAdmin bool) (*GrantResult, error) {
	if q == nil {
		return nil, fmt.Errorf("permission: queryer is nil")
	}
	if tenantID <= 0 || employeeID <= 0 {
		return nil, fmt.Errorf("permission: tenant_id and employee_id are required")
	}
	var spec *productGrantSpec
	for i := range productGrantSpecs {
		if productGrantSpecs[i].Product == product {
			spec = &productGrantSpecs[i]
			break
		}
	}
	if spec == nil {
		return nil, fmt.Errorf("permission: unknown product %q", product)
	}
	if spec.SkipReason != "" {
		return &GrantResult{Product: product, Status: GrantStatusSkipped, Reason: spec.SkipReason}, nil
	}
	exists, err := tableExists(ctx, q, spec.Table)
	if err != nil {
		return nil, err
	}
	if !exists {
		return &GrantResult{Product: product, Table: spec.Table, Status: GrantStatusSkipped, Reason: "table_missing"}, nil
	}
	var actor *int64
	if actorID > 0 {
		value := actorID
		actor = &value
	}
	if !isAdmin {
		tag, err := disableProductGrant(ctx, q, spec, tenantID, employeeID, actor)
		if err != nil {
			return nil, err
		}
		status := GrantStatusExists
		if tag.RowsAffected() > 0 {
			status = GrantStatusGranted
		}
		return &GrantResult{Product: product, Table: spec.Table, Status: status}, nil
	}
	tag, err := upsertAdminGrant(ctx, q, *spec, tenantID, employeeID, actor)
	if err != nil {
		return nil, err
	}
	status := GrantStatusExists
	if tag.RowsAffected() > 0 {
		status = GrantStatusGranted
	}
	return &GrantResult{Product: product, Table: spec.Table, Status: status}, nil
}

func upsertAdminGrant(ctx context.Context, q QueryExecer, spec productGrantSpec, tenantID, employeeID int64, actor *int64) (pgconn.CommandTag, error) {
	raw, err := json.Marshal(spec.Abilities)
	if err != nil {
		return pgconn.CommandTag{}, fmt.Errorf("permission: encode %s admin abilities: %w", spec.Product, err)
	}
	switch spec.Product {
	case ProductEMR:
		return q.Exec(ctx, `
			INSERT INTO emr_permission_assignments
				(tenant_id, employee_id, enabled, emr_role_code, scope, abilities_json, created_by, updated_by)
			VALUES ($1, $2, TRUE, $3, 'tenant', $4::jsonb, $5, $5)
			ON CONFLICT (tenant_id, employee_id)
			DO UPDATE SET enabled = TRUE, emr_role_code = EXCLUDED.emr_role_code, scope = 'tenant',
				abilities_json = EXCLUDED.abilities_json, updated_by = EXCLUDED.updated_by, updated_at = NOW()
		`, tenantID, employeeID, spec.RoleCode, string(raw), actor)
	case ProductPatientOps:
		return q.Exec(ctx, `
			INSERT INTO patient_ops_permission_assignments
				(tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
			VALUES ($1, $2, TRUE, $3, $4::jsonb, $5, $5)
			ON CONFLICT (tenant_id, employee_id)
			DO UPDATE SET enabled = TRUE, role_code = EXCLUDED.role_code,
				abilities_json = EXCLUDED.abilities_json, updated_by = EXCLUDED.updated_by, updated_at = NOW()
		`, tenantID, employeeID, spec.RoleCode, string(raw), actor)
	case ProductDealExpert:
		return q.Exec(ctx, `
			INSERT INTO deal_expert_permission_assignments
				(tenant_id, employee_id, enabled, role_code, abilities_json, data_scope, visible_employee_ids, created_by, updated_by)
			VALUES ($1, $2, TRUE, $3, $4::jsonb, 'tenant', '{}'::bigint[], $5, $5)
			ON CONFLICT (tenant_id, employee_id)
			DO UPDATE SET enabled = TRUE, role_code = EXCLUDED.role_code, data_scope = 'tenant',
				abilities_json = EXCLUDED.abilities_json, updated_by = EXCLUDED.updated_by, updated_at = NOW()
		`, tenantID, employeeID, spec.RoleCode, string(raw), actor)
	case ProductCompliance, ProductFollowup:
		table := "compliance_permission_assignments"
		if spec.Product == ProductFollowup {
			table = "followup_permission_assignments"
		}
		return q.Exec(ctx, fmt.Sprintf(`
			INSERT INTO %s
				(tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
			VALUES ($1, $2, TRUE, $3, $4::jsonb, $5, $5)
			ON CONFLICT (tenant_id, employee_id)
			DO UPDATE SET enabled = TRUE, role_code = EXCLUDED.role_code,
				abilities_json = EXCLUDED.abilities_json, updated_by = EXCLUDED.updated_by, updated_at = NOW()
		`, table), tenantID, employeeID, spec.RoleCode, string(raw), actor)
	case ProductContent:
		return q.Exec(ctx, `
			INSERT INTO content_permission_assignments
				(tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
			VALUES ($1, $2, TRUE, $3, $4::jsonb, $5, $5)
			ON CONFLICT (tenant_id, employee_id)
			DO UPDATE SET enabled = TRUE, role_code = EXCLUDED.role_code,
				abilities_json = EXCLUDED.abilities_json, updated_by = EXCLUDED.updated_by, updated_at = NOW()
		`, tenantID, employeeID, spec.RoleCode, string(raw), actor)
	default:
		return pgconn.CommandTag{}, fmt.Errorf("permission: unsupported product %q", spec.Product)
	}
}

func disableProductGrant(ctx context.Context, q QueryExecer, spec *productGrantSpec, tenantID, employeeID int64, actor *int64) (pgconn.CommandTag, error) {
	return q.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET enabled = FALSE, updated_by = $3, updated_at = NOW()
		WHERE tenant_id = $1 AND employee_id = $2
	`, spec.Table), tenantID, employeeID, actor)
}

// EnsureInstitutionAdminGrants 给一个机构管理员补齐六个产品的默认管理员权限。
//
// 幂等：每张表都是 INSERT ... ON CONFLICT (tenant_id, employee_id) DO NOTHING，
// 已存在的人工配置（包括被停用、被降为 staff 的行）一律不覆盖。
// dryRun 只查询是否存在，不写库。
//
// 返回每个产品的处理结果；表尚未迁移的产品返回 skipped + reason，不阻断其它产品。
func EnsureInstitutionAdminGrants(ctx context.Context, q QueryExecer, tenantID, employeeID, actorID int64, dryRun bool) ([]GrantResult, error) {
	if q == nil {
		return nil, fmt.Errorf("permission: queryer is nil")
	}
	if tenantID <= 0 || employeeID <= 0 {
		return nil, fmt.Errorf("permission: tenant_id and employee_id are required")
	}
	var actor *int64
	if actorID > 0 {
		value := actorID
		actor = &value
	}

	results := make([]GrantResult, 0, len(productGrantSpecs))
	for _, spec := range productGrantSpecs {
		if spec.SkipReason != "" {
			results = append(results, GrantResult{Product: spec.Product, Status: GrantStatusSkipped, Reason: spec.SkipReason})
			continue
		}
		exists, err := tableExists(ctx, q, spec.Table)
		if err != nil {
			return nil, err
		}
		if !exists {
			results = append(results, GrantResult{
				Product: spec.Product,
				Table:   spec.Table,
				Status:  GrantStatusSkipped,
				Reason:  "table_missing",
			})
			continue
		}
		if dryRun {
			granted, err := assignmentExists(ctx, q, spec.Table, tenantID, employeeID)
			if err != nil {
				return nil, err
			}
			status := GrantStatusWouldGrant
			if granted {
				status = GrantStatusExists
			}
			results = append(results, GrantResult{Product: spec.Product, Table: spec.Table, Status: status})
			continue
		}
		tag, err := insertAdminGrant(ctx, q, spec, tenantID, employeeID, actor)
		if err != nil {
			return nil, err
		}
		status := GrantStatusExists
		if tag.RowsAffected() > 0 {
			status = GrantStatusGranted
		}
		results = append(results, GrantResult{Product: spec.Product, Table: spec.Table, Status: status})
	}
	return results, nil
}

func tableExists(ctx context.Context, q Querier, table string) (bool, error) {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
		return false, fmt.Errorf("permission: check table %s: %w", table, err)
	}
	return exists, nil
}

func assignmentExists(ctx context.Context, q Querier, table string, tenantID, employeeID int64) (bool, error) {
	var exists bool
	query := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE tenant_id = $1 AND employee_id = $2)`, table)
	if err := q.QueryRow(ctx, query, tenantID, employeeID).Scan(&exists); err != nil {
		return false, fmt.Errorf("permission: check existing grant in %s: %w", table, err)
	}
	return exists, nil
}

func insertAdminGrant(ctx context.Context, q QueryExecer, spec productGrantSpec, tenantID, employeeID int64, actor *int64) (pgconn.CommandTag, error) {
	raw, err := json.Marshal(spec.Abilities)
	if err != nil {
		return pgconn.CommandTag{}, fmt.Errorf("permission: encode %s admin abilities: %w", spec.Product, err)
	}
	var tag pgconn.CommandTag
	switch spec.Product {
	case ProductEMR:
		tag, err = q.Exec(ctx, `
			INSERT INTO emr_permission_assignments
				(tenant_id, employee_id, enabled, emr_role_code, scope, abilities_json, created_by, updated_by)
			VALUES ($1, $2, TRUE, $3, 'tenant', $4::jsonb, $5, $5)
			ON CONFLICT (tenant_id, employee_id) DO NOTHING
		`, tenantID, employeeID, spec.RoleCode, string(raw), actor)
	case ProductPatientOps:
		tag, err = q.Exec(ctx, `
			INSERT INTO patient_ops_permission_assignments
				(tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
			VALUES ($1, $2, TRUE, $3, $4::jsonb, $5, $5)
			ON CONFLICT (tenant_id, employee_id) DO NOTHING
		`, tenantID, employeeID, spec.RoleCode, string(raw), actor)
	case ProductDealExpert:
		tag, err = q.Exec(ctx, `
			INSERT INTO deal_expert_permission_assignments
				(tenant_id, employee_id, enabled, role_code, abilities_json, data_scope, visible_employee_ids, created_by, updated_by)
			VALUES ($1, $2, TRUE, $3, $4::jsonb, 'tenant', '{}'::bigint[], $5, $5)
			ON CONFLICT (tenant_id, employee_id) DO NOTHING
		`, tenantID, employeeID, spec.RoleCode, string(raw), actor)
	case ProductContent:
		tag, err = q.Exec(ctx, `
			INSERT INTO content_permission_assignments
				(tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
			VALUES ($1, $2, TRUE, $3, $4::jsonb, $5, $5)
			ON CONFLICT (tenant_id, employee_id) DO NOTHING
		`, tenantID, employeeID, spec.RoleCode, string(raw), actor)
	case ProductCompliance, ProductFollowup:
		table := "compliance_permission_assignments"
		if spec.Product == ProductFollowup {
			table = "followup_permission_assignments"
		}
		query := fmt.Sprintf(`
			INSERT INTO %s
				(tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
			VALUES ($1, $2, TRUE, $3, $4::jsonb, $5, $5)
			ON CONFLICT (tenant_id, employee_id) DO NOTHING
		`, table)
		tag, err = q.Exec(ctx, query, tenantID, employeeID, spec.RoleCode, string(raw), actor)
	default:
		return pgconn.CommandTag{}, fmt.Errorf("permission: unsupported product %q", spec.Product)
	}
	if err != nil {
		return pgconn.CommandTag{}, fmt.Errorf("permission: grant %s admin permission: %w", spec.Product, err)
	}
	return tag, nil
}

// InstitutionAdmin 是回填时选中的主管理员。
type InstitutionAdmin struct {
	EmployeeID int64  `json:"employee_id"`
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	IsActive   bool   `json:"is_active"`
	// Source 为 is_primary（显式主管理员）或 admin_role（兜底：启用中的 admin 角色员工）。
	Source string `json:"source"`
}

// FindInstitutionPrimaryAdmin 选出一个机构用于默认授权的主管理员：
//  1. employees.is_primary = true 的员工；
//  2. 没有显式主管理员时，兜底取 id 最小的启用 admin 角色员工（与 20261112_011 迁移口径一致）。
//
// 返回 nil 表示该机构没有可用的管理员（命令会记录并跳过）。
func FindInstitutionPrimaryAdmin(ctx context.Context, q Querier, tenantID int64) (*InstitutionAdmin, error) {
	if q == nil {
		return nil, fmt.Errorf("permission: queryer is nil")
	}
	if tenantID <= 0 {
		return nil, nil
	}
	var hasPrimary bool
	if err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'employees' AND column_name = 'is_primary'
		)
	`).Scan(&hasPrimary); err != nil {
		return nil, fmt.Errorf("permission: check employees.is_primary: %w", err)
	}
	baseSelect := `
		SELECT e.id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), NULLIF(e.username, ''), COALESCE(e.phone, ''), ''),
		       COALESCE(e.phone, ''),
		       CASE WHEN e.is_active::text IN ('1','t','true','TRUE','yes') THEN TRUE ELSE FALSE END,
		       %s
		FROM employees e
		WHERE e.tenant_id = $1
		  AND e.deleted_at IS NULL
		  %s
		ORDER BY e.id
		LIMIT 1
	`
	if hasPrimary {
		var item InstitutionAdmin
		query := fmt.Sprintf(baseSelect, `'is_primary'`, `AND e.is_primary = TRUE`)
		err := q.QueryRow(ctx, query, tenantID).Scan(&item.EmployeeID, &item.Name, &item.Phone, &item.IsActive, &item.Source)
		if err == nil {
			return &item, nil
		}
		if err != pgx.ErrNoRows {
			return nil, fmt.Errorf("permission: load primary institution admin: %w", err)
		}
	}
	var item InstitutionAdmin
	query := fmt.Sprintf(baseSelect, `'admin_role'`, `
		AND CASE WHEN e.is_active::text IN ('1','t','true','TRUE','yes') THEN TRUE ELSE FALSE END
		AND (
		    lower(trim(COALESCE(e.role, ''))) = 'admin'
		    OR EXISTS (
		        SELECT 1 FROM institution_employee_roles ier
		        WHERE ier.employee_id = e.id
		          AND ier.tenant_id = e.tenant_id
		          AND lower(trim(COALESCE(ier.role_code, ''))) = 'admin'
		    )
		)`)
	err := q.QueryRow(ctx, query, tenantID).Scan(&item.EmployeeID, &item.Name, &item.Phone, &item.IsActive, &item.Source)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("permission: load fallback institution admin: %w", err)
	}
	return &item, nil
}

// PrimaryAdminSourceLabel 用于命令输出。
func PrimaryAdminSourceLabel(source string) string {
	switch strings.TrimSpace(source) {
	case "is_primary":
		return "主管理员(is_primary)"
	case "admin_role":
		return "兜底 admin 角色"
	default:
		return source
	}
}

// DefaultRoleMenuSeeds 是机构标准角色的最小默认菜单集合。
// 目的：医生/咨询师等角色开箱即可访问自己的录音列表，避免建档后仍 403。
var DefaultRoleMenuSeeds = map[string][]string{
	"doctor":     {"doctor_recordings"},
	"consultant": {"consultant_recordings"},
	"therapist":  {"therapist_recordings"},
	"frontdesk":  {"frontdesk_recordings"},
}

// SeedDefaultRoleMenus 给机构里已存在的标准角色补默认菜单，幂等。
func SeedDefaultRoleMenus(ctx context.Context, q QueryExecer, tenantID int64) error {
	if q == nil {
		return fmt.Errorf("permission: queryer is nil")
	}
	if tenantID <= 0 {
		return fmt.Errorf("permission: tenant id is required")
	}
	for roleCode, menuCodes := range DefaultRoleMenuSeeds {
		for _, menuCode := range menuCodes {
			_, err := q.Exec(ctx, `
				INSERT INTO institution_role_menus (tenant_id, role_code, menu_code, created_at, updated_at)
				SELECT $1::bigint, $2::text, m.code, NOW(), NOW()
				FROM institution_menus m
				WHERE m.deleted_at IS NULL
				  AND lower(m.code) = lower($3::text)
				  AND (m.tenant_id = $1 OR m.tenant_id IS NULL)
				  AND COALESCE(m.is_active, true) = true
				  AND EXISTS (
					SELECT 1 FROM institution_roles r
					WHERE r.tenant_id = $1
					  AND lower(r.code) = lower($2::text)
					  AND r.deleted_at IS NULL
				  )
				ON CONFLICT (tenant_id, role_code, menu_code) DO NOTHING
			`, tenantID, roleCode, menuCode)
			if err != nil {
				return fmt.Errorf("seed role menu %s/%s: %w", roleCode, menuCode, err)
			}
		}
	}
	return nil
}
