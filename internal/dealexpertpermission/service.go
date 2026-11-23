package dealexpertpermission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/permission"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) List(ctx context.Context, tenantID int64) ([]*EmployeePermissionRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id,
		       COALESCE(e.username, ''),
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '未知员工'),
		       COALESCE(e.phone, ''),
		       COALESCE(e.department_id, 0),
		       COALESCE(d.name, ''),
		       COALESCE(e.role, ''),
		       CASE WHEN e.is_active::text IN ('1','t','true','TRUE','yes') THEN true ELSE false END,
		       COALESCE((
		           SELECT json_agg(DISTINCT lower(trim(ier.role_code)))
		           FROM institution_employee_roles ier
		           WHERE ier.employee_id = e.id AND ier.tenant_id = e.tenant_id
		             AND trim(COALESCE(ier.role_code, '')) <> ''
		       ), '[]'::json) AS role_codes,
		       dp.id, dp.tenant_id, dp.employee_id, COALESCE(dp.enabled, false),
		       COALESCE(dp.role_code, ''), COALESCE(dp.abilities_json, '[]'::jsonb),
		       COALESCE(dp.data_scope, 'self'), COALESCE(dp.visible_employee_ids, '{}'::bigint[]),
		       dp.created_by, dp.updated_by, dp.created_at, dp.updated_at
		FROM employees e
		LEFT JOIN departments d ON d.id = e.department_id AND d.tenant_id = e.tenant_id AND d.deleted_at IS NULL
		LEFT JOIN deal_expert_permission_assignments dp ON dp.tenant_id = e.tenant_id AND dp.employee_id = e.id
		WHERE e.tenant_id = $1 AND e.deleted_at IS NULL
		ORDER BY e.created_at DESC, e.id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list deal expert permissions: %w", err)
	}
	defer rows.Close()

	items := make([]*EmployeePermissionRow, 0)
	for rows.Next() {
		var item EmployeePermissionRow
		var id, assignmentTenant, assignmentEmployee *int64
		var enabled bool
		var role string
		var roleCodesRaw []byte
		var abilitiesRaw []byte
		var dataScope string
		var visibleEmployeeIDs []int64
		var createdBy, updatedBy *int64
		var createdAt, updatedAt *time.Time
		if err := rows.Scan(&item.EmployeeID, &item.Username, &item.FullName, &item.Phone, &item.DepartmentID, &item.Department, &item.Role, &item.IsActive, &roleCodesRaw,
			&id, &assignmentTenant, &assignmentEmployee, &enabled, &role, &abilitiesRaw, &dataScope, &visibleEmployeeIDs, &createdBy, &updatedBy,
			&createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan deal expert permission row: %w", err)
		}
		if len(roleCodesRaw) > 0 {
			_ = json.Unmarshal(roleCodesRaw, &item.RoleCodes)
		}
		if id != nil && assignmentTenant != nil && assignmentEmployee != nil {
			abilities := []string{}
			if len(abilitiesRaw) > 0 {
				if err := json.Unmarshal(abilitiesRaw, &abilities); err != nil {
					return nil, fmt.Errorf("decode deal expert abilities: %w", err)
				}
			}
			item.Permission = &Assignment{
				ID:                 *id,
				TenantID:           *assignmentTenant,
				EmployeeID:         *assignmentEmployee,
				Enabled:            enabled,
				RoleCode:           role,
				Abilities:          normalizeAbilities(abilities),
				DataScope:          NormalizeDataScope(dataScope),
				VisibleEmployeeIDs: NormalizeVisibleEmployeeIDs(visibleEmployeeIDs),
				CreatedBy:          createdBy,
				UpdatedBy:          updatedBy,
			}
			if createdAt != nil {
				item.Permission.CreatedAt = *createdAt
			}
			if updatedAt != nil {
				item.Permission.UpdatedAt = *updatedAt
			}
		}
		items = append(items, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deal expert permissions: %w", err)
	}
	// 机构管理员即使没有 deal_expert_permission_assignments 行，也按管理员口径展示。
	adminIDs, err := permission.ListInstitutionAdminIDs(ctx, s.pool, tenantID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if _, ok := adminIDs[item.EmployeeID]; ok {
			item.Permission = adminAssignmentView(tenantID, item.EmployeeID, item.Permission)
		}
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, tenantID, employeeID int64) (*Assignment, error) {
	// 机构管理员始终按管理员权限返回，不再要求单独写 deal_expert_permission_assignments。
	if admin, err := s.IsInstitutionAdmin(ctx, tenantID, employeeID); err != nil {
		return nil, err
	} else if admin {
		existing, err := s.getAssignmentRecord(ctx, tenantID, employeeID)
		if err != nil {
			return nil, err
		}
		return adminAssignmentView(tenantID, employeeID, existing), nil
	}
	return s.getAssignmentRecord(ctx, tenantID, employeeID)
}

func (s *Service) getAssignmentRecord(ctx context.Context, tenantID, employeeID int64) (*Assignment, error) {
	var item Assignment
	var raw []byte
	var visibleEmployeeIDs []int64
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, employee_id, enabled, role_code, COALESCE(abilities_json, '[]'::jsonb),
		       COALESCE(data_scope, 'self'), COALESCE(visible_employee_ids, '{}'::bigint[]),
		       created_by, updated_by, created_at, updated_at
		FROM deal_expert_permission_assignments
		WHERE tenant_id=$1 AND employee_id=$2
	`, tenantID, employeeID).Scan(&item.ID, &item.TenantID, &item.EmployeeID, &item.Enabled, &item.RoleCode, &raw,
		&item.DataScope, &visibleEmployeeIDs, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get deal expert permission: %w", err)
	}
	if err := json.Unmarshal(raw, &item.Abilities); err != nil {
		return nil, fmt.Errorf("decode deal expert permission: %w", err)
	}
	item.Abilities = normalizeAbilities(item.Abilities)
	item.DataScope = NormalizeDataScope(item.DataScope)
	item.VisibleEmployeeIDs = NormalizeVisibleEmployeeIDs(visibleEmployeeIDs)
	return &item, nil
}

// adminAssignmentView 把机构管理员的生效权限收敛为「启用 + deal_admin + 全机构」。
func adminAssignmentView(tenantID, employeeID int64, existing *Assignment) *Assignment {
	if existing == nil {
		return DefaultAdminAssignment(tenantID, employeeID)
	}
	existing.TenantID = tenantID
	existing.EmployeeID = employeeID
	existing.Enabled = true
	existing.RoleCode = RoleAdmin
	existing.Abilities = append([]string{}, AllAbilities...)
	existing.DataScope = DataScopeTenant
	existing.VisibleEmployeeIDs = []int64{}
	return existing
}

func (s *Service) EmployeeExists(ctx context.Context, tenantID, employeeID int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employees WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL)`, employeeID, tenantID).Scan(&exists)
	return exists, err
}

// EmployeesExist 校验一组员工都属于当前机构且未删除。
func (s *Service) EmployeesExist(ctx context.Context, tenantID int64, employeeIDs []int64) (bool, error) {
	if len(employeeIDs) == 0 {
		return true, nil
	}
	var count int
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM employees
		WHERE tenant_id=$1 AND deleted_at IS NULL AND id = ANY($2::bigint[])
	`, tenantID, employeeIDs).Scan(&count); err != nil {
		return false, fmt.Errorf("check visible employees: %w", err)
	}
	return count == len(employeeIDs), nil
}

// employeeIdentity 是权限判定统一使用的机构身份快照。
//
// 管理员判定同时认两个来源：
//  1. employees.role = 'admin'（历史租户管理员/存量数据）；
//  2. institution_employee_roles.role_code = 'admin'（机构 RBAC 的正式角色）。
//
// 两者任一命中即为管理员；只做精确匹配，doctor / consultant / director
// 等其它角色不会被误判为管理员。
// employeeIdentity 是共享身份判定结果的别名，保留旧类型名方便包内使用。
type employeeIdentity = permission.InstitutionIdentity

// loadEmployeeIdentity 复用 internal/permission 的统一口径：
// employees.role 或 institution_employee_roles.role_code 任一精确等于 admin。
// 返回 nil 表示员工不存在、已删除或不属于该租户（与 EmployeeRole 的空角色语义一致）。
func (s *Service) loadEmployeeIdentity(ctx context.Context, tenantID, employeeID int64) (*employeeIdentity, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	return permission.LoadInstitutionIdentity(ctx, s.pool, tenantID, employeeID)
}

// IsInstitutionAdmin 判断员工是否为机构管理员：employees.role 或机构角色表
// 里任一来源命中 admin 即为管理员。所有权限入口统一复用它。
func (s *Service) IsInstitutionAdmin(ctx context.Context, tenantID, employeeID int64) (bool, error) {
	identity, err := s.loadEmployeeIdentity(ctx, tenantID, employeeID)
	if err != nil {
		return false, err
	}
	return identity != nil && identity.Admin, nil
}

// EmployeeRole 返回员工在机构里的角色（employees.role）。
// 管理员判定不要直接比较该返回值，请使用 IsInstitutionAdmin。
func (s *Service) EmployeeRole(ctx context.Context, tenantID, employeeID int64) (string, error) {
	identity, err := s.loadEmployeeIdentity(ctx, tenantID, employeeID)
	if err != nil {
		return "", err
	}
	if identity == nil {
		return "", nil
	}
	return identity.Role, nil
}

// VisibilityFor 解析某个员工当前生效的数据可见范围。
// 管理员（两个来源任一为 admin）固定全机构；未配置权限时按机构角色给默认
// （负责人 custom、其余 self）。
func (s *Service) VisibilityFor(ctx context.Context, tenantID, employeeID int64) (*Visibility, error) {
	identity, err := s.loadEmployeeIdentity(ctx, tenantID, employeeID)
	if err != nil {
		return nil, err
	}
	employeeRole := ""
	if identity != nil {
		employeeRole = identity.Role
	}
	if identity != nil && identity.Admin {
		return ResolveVisibility(DataScopeTenant, nil, employeeID, true), nil
	}
	assignment, err := s.Get(ctx, tenantID, employeeID)
	if err != nil {
		return nil, err
	}
	if assignment == nil {
		return ResolveVisibility(DefaultDataScopeForRole(employeeRole), nil, employeeID, false), nil
	}
	// 赢客专家管理员即使不是机构管理员，也允许使用已配置的全机构范围。
	return ResolveVisibility(assignment.DataScope, assignment.VisibleEmployeeIDs, employeeID, assignment.RoleCode == RoleAdmin), nil
}

// DefaultAbilities 返回角色的默认能力包：负责人全开，其他咨询师默认包。
func DefaultAbilities(roleCode string) []string {
	if roleCode == RoleAdmin {
		return append([]string{}, AllAbilities...)
	}
	return append([]string{}, DefaultStaffAbilities...)
}

// DefaultRoleForEmployee 未配置权限的员工按机构角色给默认角色。
func DefaultRoleForEmployee(employeeRole string) string {
	if IsManagerRole(employeeRole) {
		return RoleAdmin
	}
	return RoleStaff
}

func (s *Service) Upsert(ctx context.Context, tenantID, employeeID, actorID int64, req UpsertRequest) (*Assignment, error) {
	roleCode := strings.TrimSpace(req.RoleCode)
	if roleCode == "" {
		roleCode = RoleStaff
	}
	if roleCode != RoleAdmin && roleCode != RoleStaff {
		return nil, fmt.Errorf("invalid role_code")
	}
	abilities := normalizeAbilities(req.Abilities)
	if req.Enabled && len(abilities) == 0 {
		return nil, fmt.Errorf("abilities is required when deal expert access is enabled")
	}
	exists, err := s.EmployeeExists(ctx, tenantID, employeeID)
	if err != nil {
		return nil, fmt.Errorf("check employee: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("employee not found")
	}
	targetIdentity, err := s.loadEmployeeIdentity(ctx, tenantID, employeeID)
	if err != nil {
		return nil, err
	}
	employeeRole := ""
	if targetIdentity != nil {
		employeeRole = targetIdentity.Role
	}
	isAdmin := targetIdentity != nil && targetIdentity.Admin

	existing, err := s.Get(ctx, tenantID, employeeID)
	if err != nil {
		return nil, err
	}

	dataScope := ""
	visibleEmployeeIDs := []int64{}
	if strings.TrimSpace(req.DataScope) != "" {
		dataScope = NormalizeDataScope(req.DataScope)
		visibleEmployeeIDs = NormalizeVisibleEmployeeIDs(req.VisibleEmployeeIDs)
	} else if existing != nil {
		// 旧客户端不传 data_scope 时保留原配置，避免保存能力时把可见范围重置。
		dataScope = NormalizeDataScope(existing.DataScope)
		visibleEmployeeIDs = NormalizeVisibleEmployeeIDs(existing.VisibleEmployeeIDs)
	} else {
		dataScope = DefaultDataScopeForRole(employeeRole)
		visibleEmployeeIDs = NormalizeVisibleEmployeeIDs(req.VisibleEmployeeIDs)
	}

	if isAdmin {
		// 机构管理员固定全机构，忽略请求里的自定义范围。
		dataScope = DataScopeTenant
		visibleEmployeeIDs = []int64{}
	}
	switch dataScope {
	case DataScopeTenant:
		// 赢客专家的产品管理员也可以配置全机构可见范围；
		// 不再要求目标员工同时是机构管理员。
		if !isAdmin && roleCode != RoleAdmin {
			return nil, fmt.Errorf("全机构可见范围仅限机构管理员或赢客专家管理员")
		}
		visibleEmployeeIDs = []int64{}
	case DataScopeSelf:
		visibleEmployeeIDs = []int64{}
	case DataScopeCustom:
		if len(visibleEmployeeIDs) == 0 {
			return nil, fmt.Errorf("指定人员可见范围至少需要选择 1 名员工")
		}
		ok, err := s.EmployeesExist(ctx, tenantID, visibleEmployeeIDs)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("指定人员不存在或不属于当前机构")
		}
	default:
		return nil, fmt.Errorf("invalid data_scope")
	}

	raw, err := json.Marshal(abilities)
	if err != nil {
		return nil, err
	}
	var item Assignment
	var stored []byte
	var storedVisible []int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO deal_expert_permission_assignments
			(tenant_id, employee_id, enabled, role_code, abilities_json, data_scope, visible_employee_ids, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7::bigint[],$8,$8)
		ON CONFLICT (tenant_id, employee_id) DO UPDATE SET
		  enabled=EXCLUDED.enabled, role_code=EXCLUDED.role_code, abilities_json=EXCLUDED.abilities_json,
		  data_scope=EXCLUDED.data_scope, visible_employee_ids=EXCLUDED.visible_employee_ids,
		  updated_by=EXCLUDED.updated_by, updated_at=NOW()
		RETURNING id, tenant_id, employee_id, enabled, role_code, abilities_json, data_scope,
		          visible_employee_ids, created_by, updated_by, created_at, updated_at
	`, tenantID, employeeID, req.Enabled, roleCode, string(raw), dataScope, visibleEmployeeIDs, actorID).Scan(
		&item.ID, &item.TenantID, &item.EmployeeID, &item.Enabled, &item.RoleCode, &stored, &item.DataScope,
		&storedVisible, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert deal expert permission: %w", err)
	}
	if err := json.Unmarshal(stored, &item.Abilities); err != nil {
		return nil, err
	}
	item.Abilities = normalizeAbilities(item.Abilities)
	item.DataScope = NormalizeDataScope(item.DataScope)
	item.VisibleEmployeeIDs = NormalizeVisibleEmployeeIDs(storedVisible)
	return &item, nil
}

// AccessFor 返回当前员工的生效权限：机构管理员（employees.role 或机构角色表
// 任一来源为 admin）永远全能力，避免锁死；未配置权限的员工按机构角色给默认
// （负责人全开，其余 deal_staff 默认包）。
func (s *Service) AccessFor(ctx context.Context, claims *auth.Claims, tenantID int64) (*Access, error) {
	if claims == nil {
		return nil, fmt.Errorf("invalid token")
	}
	if claims.UserType == auth.UserTypeAdmin {
		return &Access{UserID: claims.UserID, TenantID: tenantID, Admin: true, Enabled: true, RoleCode: RoleAdmin,
			Abilities: abilitySet(AllAbilities), DataScope: DataScopeTenant, VisibleEmployeeIDs: []int64{}}, nil
	}
	if claims.TenantID == nil || *claims.TenantID != tenantID {
		return nil, fmt.Errorf("access denied")
	}
	identity, err := s.loadEmployeeIdentity(ctx, tenantID, claims.UserID)
	if err != nil {
		return nil, err
	}
	employeeRole := ""
	if identity != nil {
		employeeRole = identity.Role
	}
	if identity != nil && identity.Admin {
		return &Access{UserID: claims.UserID, TenantID: tenantID, Admin: true, Enabled: true, RoleCode: RoleAdmin,
			Abilities: abilitySet(AllAbilities), DataScope: DataScopeTenant, VisibleEmployeeIDs: []int64{}}, nil
	}
	assignment, err := s.Get(ctx, tenantID, claims.UserID)
	if err != nil {
		return nil, err
	}
	if assignment == nil {
		roleCode := DefaultRoleForEmployee(employeeRole)
		abilities := DefaultAbilities(roleCode)
		visibility := ResolveVisibility(DefaultDataScopeForRole(employeeRole), nil, claims.UserID, false)
		return &Access{UserID: claims.UserID, TenantID: tenantID, Enabled: true, RoleCode: roleCode,
			Abilities: abilitySet(abilities), DataScope: visibility.Scope, VisibleEmployeeIDs: visibility.EmployeeIDs}, nil
	}
	visibility := ResolveVisibility(assignment.DataScope, assignment.VisibleEmployeeIDs, claims.UserID, assignment.RoleCode == RoleAdmin)
	if !assignment.Enabled {
		return &Access{UserID: claims.UserID, TenantID: tenantID, Enabled: false, RoleCode: assignment.RoleCode,
			Abilities: map[string]struct{}{}, DataScope: visibility.Scope, VisibleEmployeeIDs: visibility.EmployeeIDs}, nil
	}
	return &Access{UserID: claims.UserID, TenantID: tenantID, Enabled: true, RoleCode: assignment.RoleCode,
		Abilities: abilitySet(assignment.Abilities), DataScope: visibility.Scope, VisibleEmployeeIDs: visibility.EmployeeIDs}, nil
}

func abilitySet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func normalizeAbilities(values []string) []string {
	known := map[string]struct{}{}
	for _, ability := range AllAbilities {
		known[ability] = struct{}{}
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, ok := known[value]; !ok {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

type UpsertRequest struct {
	Enabled            bool     `json:"enabled"`
	RoleCode           string   `json:"role_code"`
	Abilities          []string `json:"abilities"`
	DataScope          string   `json:"data_scope"`
	VisibleEmployeeIDs []int64  `json:"visible_employee_ids"`
}
