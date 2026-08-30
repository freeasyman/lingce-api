package emrpermission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ListEmployeePermissions(ctx context.Context, tenantID int64) ([]*EmployeePermissionRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			e.id,
			COALESCE(e.username, '') AS username,
			COALESCE(
				NULLIF(NULLIF(e.full_name, 'unknown'), ''),
				NULLIF(NULLIF(e.name, 'unknown'), ''),
				NULLIF(e.username, ''),
				NULLIF(e.phone, ''),
				'未知员工'
			) AS full_name,
			COALESCE(e.phone, '') AS phone,
			COALESCE(d.name, '') AS department_name,
			COALESCE((
				SELECT lower(er.role_code)
				FROM institution_employee_roles er
				WHERE er.employee_id = e.id
				  AND er.tenant_id = e.tenant_id
				ORDER BY er.updated_at DESC NULLS LAST, er.created_at DESC, er.id DESC
				LIMIT 1
			), '') AS org_role_code,
			COALESCE((
				SELECT COALESCE(
					(
						SELECT ir.name
						FROM institution_roles ir
						WHERE ir.tenant_id = e.tenant_id
						  AND lower(ir.code) = lower(er.role_code)
						  AND ir.deleted_at IS NULL
						ORDER BY ir.id DESC
						LIMIT 1
					),
					(
						SELECT NULLIF(r.name_cn, '')
						FROM inst_roles r
						WHERE lower(r.code) = lower(er.role_code)
						ORDER BY r.code
						LIMIT 1
					),
					er.role_code
				)
				FROM institution_employee_roles er
				WHERE er.employee_id = e.id
				  AND er.tenant_id = e.tenant_id
				ORDER BY er.updated_at DESC NULLS LAST, er.created_at DESC, er.id DESC
				LIMIT 1
			), '') AS org_role_name,
			CASE
				WHEN e.is_active::text IN ('1','t','true','TRUE','yes') THEN true
				ELSE false
			END AS is_active,
			ep.id,
			ep.tenant_id,
			ep.employee_id,
			COALESCE(ep.enabled, false),
			COALESCE(ep.emr_role_code, ''),
			COALESCE(ep.scope, ''),
			COALESCE(ep.abilities_json, '[]'::jsonb),
			ep.created_by,
			ep.updated_by,
			ep.created_at,
			ep.updated_at
		FROM employees e
		LEFT JOIN departments d
			ON d.id = e.department_id
		   AND d.tenant_id = e.tenant_id
		   AND d.deleted_at IS NULL
		LEFT JOIN emr_permission_assignments ep
			ON ep.employee_id = e.id
		   AND ep.tenant_id = e.tenant_id
		WHERE e.tenant_id = $1
		  AND e.deleted_at IS NULL
		ORDER BY e.created_at DESC, e.id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list emr permission rows: %w", err)
	}
	defer rows.Close()

	items := make([]*EmployeePermissionRow, 0)
	for rows.Next() {
		var item EmployeePermissionRow
		var assignmentID *int64
		var assignmentTenantID *int64
		var assignmentEmployeeID *int64
		var enabled bool
		var roleCode string
		var scope string
		var abilitiesRaw []byte
		var createdBy *int64
		var updatedBy *int64
		var createdAt *time.Time
		var updatedAt *time.Time

		if err := rows.Scan(
			&item.EmployeeID,
			&item.Username,
			&item.FullName,
			&item.Phone,
			&item.DepartmentName,
			&item.OrgRoleCode,
			&item.OrgRoleName,
			&item.IsActive,
			&assignmentID,
			&assignmentTenantID,
			&assignmentEmployeeID,
			&enabled,
			&roleCode,
			&scope,
			&abilitiesRaw,
			&createdBy,
			&updatedBy,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan emr permission row: %w", err)
		}

		if assignmentID != nil && assignmentTenantID != nil && assignmentEmployeeID != nil {
			abilities := make([]string, 0)
			if len(abilitiesRaw) > 0 {
				if err := json.Unmarshal(abilitiesRaw, &abilities); err != nil {
					return nil, fmt.Errorf("failed to parse emr abilities json: %w", err)
				}
			}
			assignment := &Assignment{
				ID:          *assignmentID,
				TenantID:    *assignmentTenantID,
				EmployeeID:  *assignmentEmployeeID,
				Enabled:     enabled,
				EmrRoleCode: roleCode,
				Scope:       scope,
				Abilities:   abilities,
				CreatedBy:   createdBy,
				UpdatedBy:   updatedBy,
			}
			if createdAt != nil {
				assignment.CreatedAt = *createdAt
			}
			if updatedAt != nil {
				assignment.UpdatedAt = *updatedAt
			}
			item.EmrPermission = assignment
		}
		items = append(items, &item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate emr permission rows: %w", err)
	}
	return items, nil
}

func (s *Store) GetAssignmentByEmployeeID(ctx context.Context, tenantID, employeeID int64) (*Assignment, error) {
	var item Assignment
	var abilitiesRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, employee_id, enabled, emr_role_code, scope,
		       COALESCE(abilities_json, '[]'::jsonb), created_by, updated_by, created_at, updated_at
		FROM emr_permission_assignments
		WHERE tenant_id = $1 AND employee_id = $2
	`, tenantID, employeeID).Scan(
		&item.ID,
		&item.TenantID,
		&item.EmployeeID,
		&item.Enabled,
		&item.EmrRoleCode,
		&item.Scope,
		&abilitiesRaw,
		&item.CreatedBy,
		&item.UpdatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get emr permission assignment: %w", err)
	}
	if len(abilitiesRaw) > 0 {
		if err := json.Unmarshal(abilitiesRaw, &item.Abilities); err != nil {
			return nil, fmt.Errorf("failed to parse emr abilities json: %w", err)
		}
	}
	return &item, nil
}

func (s *Store) EmployeeExistsInTenant(ctx context.Context, tenantID, employeeID int64) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM employees
			WHERE id = $1
			  AND tenant_id = $2
			  AND deleted_at IS NULL
		)
	`, employeeID, tenantID).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check employee existence: %w", err)
	}
	return exists, nil
}

func (s *Store) UpsertAssignment(ctx context.Context, assignment *Assignment) (*Assignment, error) {
	abilitiesJSON, err := json.Marshal(assignment.Abilities)
	if err != nil {
		return nil, fmt.Errorf("failed to encode emr abilities: %w", err)
	}

	var saved Assignment
	var abilitiesRaw []byte
	err = s.pool.QueryRow(ctx, `
		INSERT INTO emr_permission_assignments (
			tenant_id, employee_id, enabled, emr_role_code, scope, abilities_json, created_by, updated_by, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, NOW(), NOW())
		ON CONFLICT (tenant_id, employee_id)
		DO UPDATE SET
			enabled = EXCLUDED.enabled,
			emr_role_code = EXCLUDED.emr_role_code,
			scope = EXCLUDED.scope,
			abilities_json = EXCLUDED.abilities_json,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
		RETURNING id, tenant_id, employee_id, enabled, emr_role_code, scope,
		          COALESCE(abilities_json, '[]'::jsonb), created_by, updated_by, created_at, updated_at
	`, assignment.TenantID, assignment.EmployeeID, assignment.Enabled, assignment.EmrRoleCode, assignment.Scope, string(abilitiesJSON), assignment.CreatedBy, assignment.UpdatedBy).Scan(
		&saved.ID,
		&saved.TenantID,
		&saved.EmployeeID,
		&saved.Enabled,
		&saved.EmrRoleCode,
		&saved.Scope,
		&abilitiesRaw,
		&saved.CreatedBy,
		&saved.UpdatedBy,
		&saved.CreatedAt,
		&saved.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert emr permission assignment: %w", err)
	}
	if len(abilitiesRaw) > 0 {
		if err := json.Unmarshal(abilitiesRaw, &saved.Abilities); err != nil {
			return nil, fmt.Errorf("failed to parse emr abilities json after save: %w", err)
		}
	}
	return &saved, nil
}

func normalizeAbilities(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		v := strings.TrimSpace(value)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}
