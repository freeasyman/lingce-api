package followuppermission

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
		       CASE WHEN e.is_active::text IN ('1','t','true','TRUE','yes') THEN true ELSE false END,
		       fp.id, fp.tenant_id, fp.employee_id, COALESCE(fp.enabled, false),
		       COALESCE(fp.role_code, ''), COALESCE(fp.abilities_json, '[]'::jsonb),
		       fp.created_by, fp.updated_by, fp.created_at, fp.updated_at
		FROM employees e
		LEFT JOIN departments d ON d.id = e.department_id AND d.tenant_id = e.tenant_id AND d.deleted_at IS NULL
		LEFT JOIN followup_permission_assignments fp ON fp.tenant_id = e.tenant_id AND fp.employee_id = e.id
		WHERE e.tenant_id = $1 AND e.deleted_at IS NULL
		ORDER BY e.created_at DESC, e.id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list followup permissions: %w", err)
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
		if err := rows.Scan(&item.EmployeeID, &item.Username, &item.FullName, &item.Phone, &item.Department, &item.IsActive,
			&id, &assignmentTenant, &assignmentEmployee, &enabled, &role, &abilitiesRaw, &createdBy, &updatedBy,
			&createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan followup permission row: %w", err)
		}
		if id != nil && assignmentTenant != nil && assignmentEmployee != nil {
			abilities := []string{}
			if len(abilitiesRaw) > 0 {
				if err := json.Unmarshal(abilitiesRaw, &abilities); err != nil {
					return nil, fmt.Errorf("decode followup abilities: %w", err)
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
		return nil, fmt.Errorf("iterate followup permissions: %w", err)
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, tenantID, employeeID int64) (*Assignment, error) {
	var item Assignment
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, employee_id, enabled, role_code, COALESCE(abilities_json, '[]'::jsonb),
		       created_by, updated_by, created_at, updated_at
		FROM followup_permission_assignments
		WHERE tenant_id=$1 AND employee_id=$2
	`, tenantID, employeeID).Scan(&item.ID, &item.TenantID, &item.EmployeeID, &item.Enabled, &item.RoleCode, &raw, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get followup permission: %w", err)
	}
	if err := json.Unmarshal(raw, &item.Abilities); err != nil {
		return nil, fmt.Errorf("decode followup permission: %w", err)
	}
	item.Abilities = normalizeAbilities(item.Abilities)
	return &item, nil
}

func (s *Service) EmployeeExists(ctx context.Context, tenantID, employeeID int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employees WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL)`, employeeID, tenantID).Scan(&exists)
	return exists, err
}

func (s *Service) Upsert(ctx context.Context, tenantID, employeeID, actorID int64, req UpsertRequest) (*Assignment, error) {
	if req.RoleCode != RoleAdmin && req.RoleCode != RoleStaff {
		return nil, fmt.Errorf("invalid role_code")
	}
	if req.Enabled && len(req.Abilities) == 0 {
		return nil, fmt.Errorf("abilities is required when followup is enabled")
	}
	exists, err := s.EmployeeExists(ctx, tenantID, employeeID)
	if err != nil {
		return nil, fmt.Errorf("check employee: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("employee not found")
	}
	abilities, err := json.Marshal(normalizeAbilities(req.Abilities))
	if err != nil {
		return nil, err
	}
	var item Assignment
	var raw []byte
	err = s.pool.QueryRow(ctx, `
		INSERT INTO followup_permission_assignments (tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,$6)
		ON CONFLICT (tenant_id, employee_id) DO UPDATE SET
		  enabled=EXCLUDED.enabled, role_code=EXCLUDED.role_code, abilities_json=EXCLUDED.abilities_json,
		  updated_by=EXCLUDED.updated_by, updated_at=NOW()
		RETURNING id, tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by, created_at, updated_at
	`, tenantID, employeeID, req.Enabled, req.RoleCode, string(abilities), actorID).Scan(&item.ID, &item.TenantID, &item.EmployeeID, &item.Enabled, &item.RoleCode, &raw, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert followup permission: %w", err)
	}
	if err := json.Unmarshal(raw, &item.Abilities); err != nil {
		return nil, err
	}
	item.Abilities = normalizeAbilities(item.Abilities)
	return &item, nil
}

func (s *Service) Authorize(ctx context.Context, claims *auth.Claims, tenantID int64, ability string) (*Access, error) {
	if claims == nil {
		return nil, fmt.Errorf("invalid token")
	}
	if claims.UserType == auth.UserTypeAdmin {
		return &Access{UserID: claims.UserID, TenantID: tenantID, Admin: true, Enabled: true}, nil
	}
	if claims.TenantID == nil || *claims.TenantID != tenantID {
		return nil, fmt.Errorf("access denied")
	}
	assignment, err := s.Get(ctx, tenantID, claims.UserID)
	if err != nil {
		return nil, err
	}
	if assignment == nil || !assignment.Enabled {
		return nil, fmt.Errorf("followup access is disabled")
	}
	set := make(map[string]struct{}, len(assignment.Abilities))
	for _, value := range assignment.Abilities {
		set[value] = struct{}{}
	}
	access := &Access{UserID: claims.UserID, TenantID: tenantID, Enabled: true, RoleCode: assignment.RoleCode, Abilities: set}
	if !access.Has(ability) {
		return nil, fmt.Errorf("followup permission denied")
	}
	return access, nil
}

func normalizeAbilities(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
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
