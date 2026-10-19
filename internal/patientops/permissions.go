package patientops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	PatientOpsRoleAdmin = "patient_ops_admin"
	PatientOpsRoleStaff = "patient_ops_staff"

	PatientOpsAbilityTasksReadAll = "patient_ops.tasks.read_all"
	PatientOpsAbilityPermission   = "patient_ops.permission.manage"
)

var patientOpsAbilities = map[string]struct{}{
	PatientOpsAbilityTasksReadAll: {},
	PatientOpsAbilityPermission:   {},
}

type PatientOpsPermissionAssignment struct {
	ID         int64     `json:"id"`
	TenantID   int64     `json:"tenant_id"`
	EmployeeID int64     `json:"employee_id"`
	Enabled    bool      `json:"enabled"`
	RoleCode   string    `json:"role_code"`
	Abilities  []string  `json:"abilities"`
	CreatedBy  *int64    `json:"created_by,omitempty"`
	UpdatedBy  *int64    `json:"updated_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type PatientOpsPermissionEmployee struct {
	EmployeeID int64                           `json:"employee_id"`
	Name       string                          `json:"full_name"`
	Username   string                          `json:"username"`
	Phone      string                          `json:"phone"`
	Role       string                          `json:"role"`
	IsActive   bool                            `json:"is_active"`
	Permission *PatientOpsPermissionAssignment `json:"permission,omitempty"`
}

type UpsertPatientOpsPermissionRequest struct {
	Enabled   bool     `json:"enabled"`
	RoleCode  string   `json:"role_code"`
	Abilities []string `json:"abilities"`
}

func normalizePatientOpsAbilities(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, ok := patientOpsAbilities[value]; !ok {
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

func (s *Service) ListPatientOpsPermissions(ctx context.Context, tenantID int64) ([]PatientOpsPermissionEmployee, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id,
		       COALESCE(NULLIF(e.name,''), NULLIF(e.username,''), '未知员工'),
		       COALESCE(e.username,''),
		       COALESCE(e.phone,''),
		       COALESCE(e.role,''),
		       CASE WHEN e.is_active::text IN ('1','t','true','TRUE','yes') THEN true ELSE false END,
		       a.id, a.tenant_id, a.employee_id, COALESCE(a.enabled,false),
		       COALESCE(a.role_code,''), COALESCE(a.abilities_json,'[]'::jsonb),
		       a.created_by, a.updated_by, a.created_at, a.updated_at
		FROM employees e
		LEFT JOIN patient_ops_permission_assignments a
		       ON a.tenant_id=e.tenant_id AND a.employee_id=e.id
		WHERE e.tenant_id=$1 AND e.deleted_at IS NULL
		ORDER BY e.created_at DESC, e.id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list patient ops permissions: %w", err)
	}
	defer rows.Close()

	items := make([]PatientOpsPermissionEmployee, 0)
	for rows.Next() {
		var item PatientOpsPermissionEmployee
		var assignmentID, assignmentTenant, assignmentEmployee, createdBy, updatedBy *int64
		var enabled bool
		var roleCode string
		var raw []byte
		var createdAt, updatedAt *time.Time
		if err := rows.Scan(
			&item.EmployeeID, &item.Name, &item.Username, &item.Phone, &item.Role, &item.IsActive,
			&assignmentID, &assignmentTenant, &assignmentEmployee, &enabled, &roleCode, &raw,
			&createdBy, &updatedBy, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan patient ops permission: %w", err)
		}
		if assignmentID != nil && assignmentTenant != nil && assignmentEmployee != nil {
			abilities := []string{}
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &abilities)
			}
			item.Permission = &PatientOpsPermissionAssignment{
				ID:         *assignmentID,
				TenantID:   *assignmentTenant,
				EmployeeID: *assignmentEmployee,
				Enabled:    enabled,
				RoleCode:   roleCode,
				Abilities:  normalizePatientOpsAbilities(abilities),
				CreatedBy:  createdBy,
				UpdatedBy:  updatedBy,
			}
			if createdAt != nil {
				item.Permission.CreatedAt = *createdAt
			}
			if updatedAt != nil {
				item.Permission.UpdatedAt = *updatedAt
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetPatientOpsPermission(ctx context.Context, tenantID, employeeID int64) (*PatientOpsPermissionAssignment, error) {
	var item PatientOpsPermissionAssignment
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, employee_id, enabled, role_code, COALESCE(abilities_json,'[]'::jsonb),
		       created_by, updated_by, created_at, updated_at
		FROM patient_ops_permission_assignments
		WHERE tenant_id=$1 AND employee_id=$2
	`, tenantID, employeeID).Scan(
		&item.ID, &item.TenantID, &item.EmployeeID, &item.Enabled, &item.RoleCode, &raw,
		&item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get patient ops permission: %w", err)
	}
	if err := json.Unmarshal(raw, &item.Abilities); err != nil {
		return nil, fmt.Errorf("decode patient ops abilities: %w", err)
	}
	item.Abilities = normalizePatientOpsAbilities(item.Abilities)
	return &item, nil
}

func (s *Service) UpsertPatientOpsPermission(ctx context.Context, tenantID, employeeID, actorID int64, req UpsertPatientOpsPermissionRequest) (*PatientOpsPermissionAssignment, error) {
	roleCode := strings.TrimSpace(req.RoleCode)
	if roleCode == "" {
		roleCode = PatientOpsRoleStaff
	}
	if roleCode != PatientOpsRoleAdmin && roleCode != PatientOpsRoleStaff {
		return nil, fmt.Errorf("invalid role_code")
	}
	abilities := normalizePatientOpsAbilities(req.Abilities)
	if req.Enabled && len(abilities) == 0 {
		return nil, fmt.Errorf("abilities is required when enabled")
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employees WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL)`, employeeID, tenantID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check employee: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("employee not found")
	}
	raw, err := json.Marshal(abilities)
	if err != nil {
		return nil, err
	}
	var item PatientOpsPermissionAssignment
	var stored []byte
	err = s.pool.QueryRow(ctx, `
		INSERT INTO patient_ops_permission_assignments (tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,$6)
		ON CONFLICT (tenant_id, employee_id) DO UPDATE SET
			enabled=EXCLUDED.enabled,
			role_code=EXCLUDED.role_code,
			abilities_json=EXCLUDED.abilities_json,
			updated_by=EXCLUDED.updated_by,
			updated_at=NOW()
		RETURNING id, tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by, created_at, updated_at
	`, tenantID, employeeID, req.Enabled, roleCode, string(raw), actorID).Scan(
		&item.ID, &item.TenantID, &item.EmployeeID, &item.Enabled, &item.RoleCode, &stored,
		&item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert patient ops permission: %w", err)
	}
	if err := json.Unmarshal(stored, &item.Abilities); err != nil {
		return nil, err
	}
	item.Abilities = normalizePatientOpsAbilities(item.Abilities)
	return &item, nil
}

func (s *Service) HasPatientOpsPermission(ctx context.Context, tenantID, employeeID int64, ability string) (bool, error) {
	var allowed bool
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(enabled,false) AND COALESCE(abilities_json ? $3::text,false)
		FROM patient_ops_permission_assignments
		WHERE tenant_id=$1 AND employee_id=$2
	`, tenantID, employeeID, ability).Scan(&allowed)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check patient ops permission: %w", err)
	}
	return allowed, nil
}
