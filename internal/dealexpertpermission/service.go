package dealexpertpermission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
		       COALESCE(d.name, ''),
		       COALESCE(e.role, ''),
		       CASE WHEN e.is_active::text IN ('1','t','true','TRUE','yes') THEN true ELSE false END,
		       dp.id, dp.tenant_id, dp.employee_id, COALESCE(dp.enabled, false),
		       COALESCE(dp.role_code, ''), COALESCE(dp.abilities_json, '[]'::jsonb),
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
		var abilitiesRaw []byte
		var createdBy, updatedBy *int64
		var createdAt, updatedAt *time.Time
		if err := rows.Scan(&item.EmployeeID, &item.Username, &item.FullName, &item.Phone, &item.Department, &item.Role, &item.IsActive,
			&id, &assignmentTenant, &assignmentEmployee, &enabled, &role, &abilitiesRaw, &createdBy, &updatedBy,
			&createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan deal expert permission row: %w", err)
		}
		if id != nil && assignmentTenant != nil && assignmentEmployee != nil {
			abilities := []string{}
			if len(abilitiesRaw) > 0 {
				if err := json.Unmarshal(abilitiesRaw, &abilities); err != nil {
					return nil, fmt.Errorf("decode deal expert abilities: %w", err)
				}
			}
			item.Permission = &Assignment{ID: *id, TenantID: *assignmentTenant, EmployeeID: *assignmentEmployee, Enabled: enabled, RoleCode: role, Abilities: normalizeAbilities(abilities), CreatedBy: createdBy, UpdatedBy: updatedBy}
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
	return items, nil
}

func (s *Service) Get(ctx context.Context, tenantID, employeeID int64) (*Assignment, error) {
	var item Assignment
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, employee_id, enabled, role_code, COALESCE(abilities_json, '[]'::jsonb),
		       created_by, updated_by, created_at, updated_at
		FROM deal_expert_permission_assignments
		WHERE tenant_id=$1 AND employee_id=$2
	`, tenantID, employeeID).Scan(&item.ID, &item.TenantID, &item.EmployeeID, &item.Enabled, &item.RoleCode, &raw, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
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
	return &item, nil
}

func (s *Service) EmployeeExists(ctx context.Context, tenantID, employeeID int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employees WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL)`, employeeID, tenantID).Scan(&exists)
	return exists, err
}

// EmployeeRole 返回员工在机构里的角色（employees.role）。
func (s *Service) EmployeeRole(ctx context.Context, tenantID, employeeID int64) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(lower(trim(e.role)), '')
		FROM employees e WHERE e.id=$1 AND e.tenant_id=$2 AND e.deleted_at IS NULL
	`, employeeID, tenantID).Scan(&role)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load employee role: %w", err)
	}
	return role, nil
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
	raw, err := json.Marshal(abilities)
	if err != nil {
		return nil, err
	}
	var item Assignment
	var stored []byte
	err = s.pool.QueryRow(ctx, `
		INSERT INTO deal_expert_permission_assignments (tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,$6)
		ON CONFLICT (tenant_id, employee_id) DO UPDATE SET
		  enabled=EXCLUDED.enabled, role_code=EXCLUDED.role_code, abilities_json=EXCLUDED.abilities_json,
		  updated_by=EXCLUDED.updated_by, updated_at=NOW()
		RETURNING id, tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by, created_at, updated_at
	`, tenantID, employeeID, req.Enabled, roleCode, string(raw), actorID).Scan(&item.ID, &item.TenantID, &item.EmployeeID, &item.Enabled, &item.RoleCode, &stored, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert deal expert permission: %w", err)
	}
	if err := json.Unmarshal(stored, &item.Abilities); err != nil {
		return nil, err
	}
	item.Abilities = normalizeAbilities(item.Abilities)
	return &item, nil
}

// AccessFor 返回当前员工的生效权限：租户管理员（employees.role=admin）永远全能力，避免锁死；
// 未配置权限的员工按机构角色给默认（负责人全开，其余 deal_staff 默认包）。
func (s *Service) AccessFor(ctx context.Context, claims *auth.Claims, tenantID int64) (*Access, error) {
	if claims == nil {
		return nil, fmt.Errorf("invalid token")
	}
	if claims.UserType == auth.UserTypeAdmin {
		return &Access{UserID: claims.UserID, TenantID: tenantID, Admin: true, Enabled: true, RoleCode: RoleAdmin, Abilities: abilitySet(AllAbilities)}, nil
	}
	if claims.TenantID == nil || *claims.TenantID != tenantID {
		return nil, fmt.Errorf("access denied")
	}
	employeeRole, err := s.EmployeeRole(ctx, tenantID, claims.UserID)
	if err != nil {
		return nil, err
	}
	if employeeRole == "admin" {
		return &Access{UserID: claims.UserID, TenantID: tenantID, Admin: true, Enabled: true, RoleCode: RoleAdmin, Abilities: abilitySet(AllAbilities)}, nil
	}
	assignment, err := s.Get(ctx, tenantID, claims.UserID)
	if err != nil {
		return nil, err
	}
	if assignment == nil {
		roleCode := DefaultRoleForEmployee(employeeRole)
		abilities := DefaultAbilities(roleCode)
		return &Access{UserID: claims.UserID, TenantID: tenantID, Enabled: true, RoleCode: roleCode, Abilities: abilitySet(abilities)}, nil
	}
	if !assignment.Enabled {
		return &Access{UserID: claims.UserID, TenantID: tenantID, Enabled: false, RoleCode: assignment.RoleCode, Abilities: map[string]struct{}{}}, nil
	}
	return &Access{UserID: claims.UserID, TenantID: tenantID, Enabled: true, RoleCode: assignment.RoleCode, Abilities: abilitySet(assignment.Abilities)}, nil
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
	Enabled   bool     `json:"enabled"`
	RoleCode  string   `json:"role_code"`
	Abilities []string `json:"abilities"`
}
