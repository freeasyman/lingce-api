package rbac

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/menuaccess"
	"github.com/jackc/pgx/v5"
)

// Institution RBAC Store operations

func legacyInstitutionRoleID(code string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(code))
	// Keep legacy role IDs within JS safe integer range to avoid precision loss
	// when frontend sends role_id back (Number max safe integer: 2^53-1).
	v := int64(h.Sum64() & 0x001fffffffffffff) // 53 bits
	if v == 0 {
		return 1
	}
	return v
}

func legacyInstitutionRoleIDCompatOld(code string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(code))
	v := int64(h.Sum64() & 0x7fffffffffffffff)
	if v == 0 {
		return 1
	}
	return v
}

func (s *Store) hasInstitutionRolesTable(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('public.institution_roles') IS NOT NULL").Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to detect institution role table: %w", err)
	}
	return exists, nil
}

func isHiddenInstitutionRoleCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "lingce_sales", "inst_test_225052":
		return true
	default:
		return false
	}
}

func (s *Store) hasInstitutionMenusTable(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('public.institution_menus') IS NOT NULL").Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to detect institution menu table: %w", err)
	}
	return exists, nil
}

func (s *Store) hasInstitutionRoleMenusTable(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('public.institution_role_menus') IS NOT NULL").Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to detect institution role menu table: %w", err)
	}
	return exists, nil
}

func (s *Store) hasInstitutionEmployeeRolesTable(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('public.institution_employee_roles') IS NOT NULL").Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to detect institution employee role table: %w", err)
	}
	return exists, nil
}

func (s *Store) hasInstitutionDepartmentRolesTable(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('public.institution_department_roles') IS NOT NULL").Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to detect institution department role table: %w", err)
	}
	return exists, nil
}

func (s *Store) findLegacyRoleCodeByID(ctx context.Context, roleID int64) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT code
		FROM inst_roles
		WHERE lower(COALESCE(code, '')) NOT IN ('lingce_sales', 'inst_test_225052')
		  AND COALESCE(name_cn, '') <> '联调角色225052已编辑'
	`)
	if err != nil {
		return "", fmt.Errorf("failed to query legacy roles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return "", fmt.Errorf("failed to scan legacy role code: %w", err)
		}
		if legacyInstitutionRoleID(code) == roleID || legacyInstitutionRoleIDCompatOld(code) == roleID {
			return code, nil
		}
	}
	return "", pgx.ErrNoRows
}

func (s *Store) resolveMenuCodesByIDs(ctx context.Context, tenantID int64, menuIDs []int64) ([]string, error) {
	if len(menuIDs) == 0 {
		return nil, nil
	}
	institutionMenusExists, err := s.hasInstitutionMenusTable(ctx)
	if err != nil {
		return nil, err
	}
	if institutionMenusExists {
		rows, err := s.pool.Query(ctx, `
			SELECT code
			FROM institution_menus
			WHERE id = ANY($1)
			  AND deleted_at IS NULL
			  AND (tenant_id = $2 OR tenant_id IS NULL)
		`, menuIDs, tenantID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve institution menu codes: %w", err)
		}
		defer rows.Close()
		codes := make([]string, 0, len(menuIDs))
		for rows.Next() {
			var code string
			if err := rows.Scan(&code); err != nil {
				return nil, fmt.Errorf("failed to scan institution menu code: %w", err)
			}
			code = strings.TrimSpace(code)
			if code != "" {
				codes = append(codes, code)
			}
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("failed to iterate institution menu codes: %w", err)
		}
		if len(codes) == len(menuIDs) {
			return codes, nil
		}
	}

	rows, err := s.pool.Query(ctx, `
		SELECT code
		FROM inst_menus
		WHERE id = ANY($1)
	`, menuIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve legacy menu codes: %w", err)
	}
	defer rows.Close()
	codes := make([]string, 0, len(menuIDs))
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("failed to scan legacy menu code: %w", err)
		}
		code = strings.TrimSpace(code)
		if code != "" {
			codes = append(codes, code)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate legacy menu codes: %w", err)
	}
	return codes, nil
}

func normalizeInstitutionRoleCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

func (s *Store) getEmployeeTenantID(ctx context.Context, employeeID int64) (int64, error) {
	var tenantID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT tenant_id
		FROM employees
		WHERE id = $1 AND deleted_at IS NULL
	`, employeeID).Scan(&tenantID); err != nil {
		return 0, fmt.Errorf("employee not found: %w", err)
	}
	return tenantID, nil
}

func (s *Store) GetRoleGrantedMenuCodes(ctx context.Context, tenantID int64, roleCode string) (map[string]struct{}, error) {
	roleCode = normalizeInstitutionRoleCode(roleCode)
	if roleCode == "" {
		return map[string]struct{}{}, nil
	}

	granted := make(map[string]struct{})
	hasInstitutionRoleMenus, err := s.hasInstitutionRoleMenusTable(ctx)
	if err != nil {
		return nil, err
	}
	hasInstitutionMenus, err := s.hasInstitutionMenusTable(ctx)
	if err != nil {
		return nil, err
	}
	if !hasInstitutionRoleMenus || !hasInstitutionMenus {
		return nil, fmt.Errorf("institution role menu tables not prepared")
	}

	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT lower(trim(rm.menu_code)) AS menu_code
		FROM institution_role_menus rm
		JOIN institution_menus m ON lower(trim(m.code)) = lower(trim(rm.menu_code))
		WHERE rm.tenant_id = $1
		  AND lower(trim(rm.role_code)) = $2
		  AND m.deleted_at IS NULL
		  AND (m.tenant_id = $1 OR m.tenant_id IS NULL)
	`, tenantID, roleCode)
	if err != nil {
		return nil, fmt.Errorf("failed to query institution role menus: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var menuCode string
		if err := rows.Scan(&menuCode); err != nil {
			return nil, fmt.Errorf("failed to scan institution role menu code: %w", err)
		}
		if menuCode != "" {
			granted[menuCode] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate institution role menu codes: %w", err)
	}
	return granted, nil
}

// upsertInstitutionEmployeeRoleTx writes the institution permission truth row.
func (s *Store) upsertInstitutionEmployeeRoleTx(ctx context.Context, tx pgx.Tx, tenantID, employeeID int64, roleID *int64, roleCode, source string) error {
	roleCode = normalizeInstitutionRoleCode(roleCode)
	var roleIDValue interface{}
	if roleID != nil && *roleID > 0 {
		roleIDValue = *roleID
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM institution_employee_roles
		WHERE tenant_id = $1 AND employee_id = $2
	`, tenantID, employeeID); err != nil {
		return fmt.Errorf("failed to clear institution employee role mirror: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO institution_employee_roles (employee_id, role_id, tenant_id, role_code, source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
	`, employeeID, roleIDValue, tenantID, roleCode, source); err != nil {
		return fmt.Errorf("failed to set institution employee role mirror: %w", err)
	}

	return nil
}

func (s *Store) removeInstitutionEmployeeRoleTx(ctx context.Context, tx pgx.Tx, tenantID, employeeID int64) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM institution_employee_roles
		WHERE tenant_id = $1 AND employee_id = $2
	`, tenantID, employeeID); err != nil {
		return fmt.Errorf("failed to remove institution employee role mirror: %w", err)
	}
	return nil
}

func (s *Store) syncDepartmentEmployeeRolesTx(ctx context.Context, tx pgx.Tx, tenantID, departmentID int64, roleID *int64, roleCode string) error {
	roleCode = normalizeInstitutionRoleCode(roleCode)
	var roleIDValue interface{}
	if roleID != nil && *roleID > 0 {
		roleIDValue = *roleID
	}

	hasCompatTable, err := s.hasInstitutionEmployeeRolesTable(ctx)
	if err != nil {
		return err
	}
	if hasCompatTable {
		if _, err := tx.Exec(ctx, `
			DELETE FROM institution_employee_roles
			WHERE tenant_id = $1
			  AND employee_id IN (SELECT id FROM employees WHERE tenant_id = $1 AND department_id = $2 AND deleted_at IS NULL)
			  AND source = 'department'
		`, tenantID, departmentID); err != nil {
			return fmt.Errorf("failed to clear department-sourced institution employee roles: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO institution_employee_roles (employee_id, role_id, tenant_id, role_code, source, created_at, updated_at)
			SELECT e.id, $3, $1, $4, 'department', NOW(), NOW()
			FROM employees e
			WHERE e.tenant_id = $1 AND e.department_id = $2 AND e.deleted_at IS NULL
			  AND NOT EXISTS (
				  SELECT 1
				  FROM institution_employee_roles existing
				  WHERE existing.tenant_id = $1
				    AND existing.employee_id = e.id
			  )
		`, tenantID, departmentID, roleIDValue, roleCode); err != nil {
			return fmt.Errorf("failed to apply department role to institution employee roles: %w", err)
		}
	}

	return nil
}

// ListInstitutionRoles retrieves a paginated list of institution roles
func (s *Store) ListInstitutionRoles(ctx context.Context, tenantID int64, req InstitutionRoleListRequest) ([]*InstitutionRole, int, error) {
	hasNewTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return nil, 0, err
	}
	if !hasNewTable {
		query := `
			SELECT code, name_cn, description, COALESCE(is_active, true), COALESCE(created_at, NOW()), COALESCE(updated_at, created_at, NOW())
			FROM inst_roles
			WHERE lower(COALESCE(code, '')) NOT IN ('lingce_sales', 'inst_test_225052')
			  AND COALESCE(name_cn, '') <> '联调角色225052已编辑'
		`
		args := []interface{}{}
		argPos := 1

		if req.Name != "" {
			query += fmt.Sprintf(" AND name_cn ILIKE $%d", argPos)
			args = append(args, "%"+req.Name+"%")
			argPos++
		}
		if req.Code != "" {
			query += fmt.Sprintf(" AND code ILIKE $%d", argPos)
			args = append(args, "%"+req.Code+"%")
			argPos++
		}
		if req.IsActive != nil {
			query += fmt.Sprintf(" AND COALESCE(is_active, true) = $%d", argPos)
			args = append(args, *req.IsActive)
			argPos++
		}

		countQuery := "SELECT COUNT(*) FROM (" + query + ") AS count_query"
		var total int
		if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("failed to count legacy roles: %w", err)
		}

		query += " ORDER BY created_at DESC"
		offset := (req.Page - 1) * req.PageSize
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argPos, argPos+1)
		args = append(args, req.PageSize, offset)

		rows, err := s.pool.Query(ctx, query, args...)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to query legacy roles: %w", err)
		}
		defer rows.Close()

		roles := make([]*InstitutionRole, 0, req.PageSize)
		for rows.Next() {
			var code, name string
			var desc *string
			var active bool
			var createdAt, updatedAt time.Time
			if err := rows.Scan(&code, &name, &desc, &active, &createdAt, &updatedAt); err != nil {
				return nil, 0, fmt.Errorf("failed to scan legacy role: %w", err)
			}
			r := &InstitutionRole{
				ID:          legacyInstitutionRoleID(code),
				TenantID:    tenantID,
				Name:        name,
				Code:        code,
				Description: desc,
				IsActive:    active,
			}
			r.CreatedAt = createdAt
			r.UpdatedAt = updatedAt
			roles = append(roles, r)
		}
		return roles, total, nil
	}

	// Self-heal sync: keep new role table aligned with legacy role definitions
	// for tenants that previously only wrote inst_roles.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO institution_roles (tenant_id, name, code, description, is_active, created_at, updated_at)
		SELECT $1,
		       COALESCE(NULLIF(r.name_cn, ''), r.code),
		       r.code,
		       r.description,
		       COALESCE(r.is_active, true),
		       COALESCE(r.created_at, NOW()),
		       COALESCE(r.updated_at, COALESCE(r.created_at, NOW()))
		FROM inst_roles r
		WHERE lower(COALESCE(r.code, '')) NOT IN ('lingce_sales', 'inst_test_225052')
		  AND COALESCE(r.name_cn, '') <> '联调角色225052已编辑'
		ON CONFLICT (tenant_id, code) WHERE deleted_at IS NULL
		DO UPDATE SET
		    name = EXCLUDED.name,
		    description = EXCLUDED.description,
		    is_active = EXCLUDED.is_active,
		    updated_at = NOW()
	`, tenantID); err != nil {
		return nil, 0, fmt.Errorf("failed to sync institution roles: %w", err)
	}

	query := `
		SELECT id, tenant_id, name, code, description, is_active, created_at, updated_at
		FROM institution_roles
		WHERE tenant_id = $1 AND deleted_at IS NULL
		  AND lower(COALESCE(code, '')) NOT IN ('lingce_sales', 'inst_test_225052')
		  AND COALESCE(name, '') <> '联调角色225052已编辑'
	`
	args := []interface{}{tenantID}
	argPos := 2

	if req.Name != "" {
		query += fmt.Sprintf(" AND name ILIKE $%d", argPos)
		args = append(args, "%"+req.Name+"%")
		argPos++
	}

	if req.Code != "" {
		query += fmt.Sprintf(" AND code ILIKE $%d", argPos)
		args = append(args, "%"+req.Code+"%")
		argPos++
	}

	if req.IsActive != nil {
		query += fmt.Sprintf(" AND is_active = $%d", argPos)
		args = append(args, *req.IsActive)
		argPos++
	}

	// Count total
	countQuery := "SELECT COUNT(*) FROM (" + query + ") AS count_query"
	var total int
	err = s.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count roles: %w", err)
	}

	// Add pagination
	query += " ORDER BY created_at DESC"
	offset := (req.Page - 1) * req.PageSize
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query roles: %w", err)
	}
	defer rows.Close()

	var roles []*InstitutionRole
	for rows.Next() {
		var r InstitutionRole
		err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan role: %w", err)
		}
		roles = append(roles, &r)
	}

	return roles, total, nil
}

// GetInstitutionRoleByID retrieves an institution role by ID
func (s *Store) GetInstitutionRoleByID(ctx context.Context, tenantID, id int64) (*InstitutionRole, error) {
	hasNewTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return nil, err
	}
	if !hasNewTable {
		code, err := s.findLegacyRoleCodeByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("legacy role not found: %w", err)
		}
		query := `
			SELECT code, name_cn, description, COALESCE(is_active, true), COALESCE(created_at, NOW()), COALESCE(updated_at, created_at, NOW())
			FROM inst_roles
			WHERE code = $1
			  AND lower(COALESCE(code, '')) NOT IN ('lingce_sales', 'inst_test_225052')
			  AND COALESCE(name_cn, '') <> '联调角色225052已编辑'
		`
		var roleCode, name string
		var desc *string
		var active bool
		var createdAt, updatedAt time.Time
		if err := s.pool.QueryRow(ctx, query, code).Scan(&roleCode, &name, &desc, &active, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("legacy role not found: %w", err)
		}
		r := &InstitutionRole{
			ID:          legacyInstitutionRoleID(roleCode),
			TenantID:    tenantID,
			Name:        name,
			Code:        roleCode,
			Description: desc,
			IsActive:    active,
		}
		r.CreatedAt = createdAt
		r.UpdatedAt = updatedAt
		return r, nil
	}

	query := `
		SELECT id, tenant_id, name, code, description, is_active, created_at, updated_at
		FROM institution_roles
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		  AND lower(COALESCE(code, '')) NOT IN ('lingce_sales', 'inst_test_225052')
		  AND COALESCE(name, '') <> '联调角色225052已编辑'
	`

	var r InstitutionRole
	err = s.pool.QueryRow(ctx, query, id, tenantID).Scan(
		&r.ID, &r.TenantID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("role not found: %w", err)
	}

	return &r, nil
}

// CreateInstitutionRole creates a new institution role
func (s *Store) CreateInstitutionRole(ctx context.Context, tenantID int64, req CreateInstitutionRoleRequest) (*InstitutionRole, error) {
	hasNewTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return nil, err
	}
	if !hasNewTable {
		query := `
			INSERT INTO inst_roles (code, name_cn, description, is_active, created_at, updated_at)
			VALUES ($1, $2, $3, true, NOW(), NOW())
			RETURNING code, name_cn, description, COALESCE(is_active, true), COALESCE(created_at, NOW()), COALESCE(updated_at, created_at, NOW())
		`
		var code, name string
		var desc *string
		var active bool
		var createdAt, updatedAt time.Time
		if err := s.pool.QueryRow(ctx, query, req.Code, req.Name, req.Description).Scan(&code, &name, &desc, &active, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to create legacy role: %w", err)
		}
		r := &InstitutionRole{
			ID:          legacyInstitutionRoleID(code),
			TenantID:    tenantID,
			Name:        name,
			Code:        code,
			Description: desc,
			IsActive:    active,
		}
		r.CreatedAt = createdAt
		r.UpdatedAt = updatedAt
		return r, nil
	}

	query := `
		INSERT INTO institution_roles (tenant_id, name, code, description, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, true, NOW(), NOW())
		RETURNING id, tenant_id, name, code, description, is_active, created_at, updated_at
	`

	var r InstitutionRole
	err = s.pool.QueryRow(ctx, query, tenantID, req.Name, req.Code, req.Description).Scan(
		&r.ID, &r.TenantID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create role: %w", err)
	}

	return &r, nil
}

// UpdateInstitutionRole updates an institution role
func (s *Store) UpdateInstitutionRole(ctx context.Context, tenantID, id int64, req UpdateInstitutionRoleRequest) (*InstitutionRole, error) {
	hasNewTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return nil, err
	}
	if !hasNewTable {
		code, err := s.findLegacyRoleCodeByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("legacy role not found: %w", err)
		}

		query := "UPDATE inst_roles SET updated_at = NOW()"
		args := []interface{}{}
		argPos := 1
		if req.Name != nil {
			query += fmt.Sprintf(", name_cn = $%d", argPos)
			args = append(args, *req.Name)
			argPos++
		}
		if req.Description != nil {
			query += fmt.Sprintf(", description = $%d", argPos)
			args = append(args, *req.Description)
			argPos++
		}
		if req.IsActive != nil {
			query += fmt.Sprintf(", is_active = $%d", argPos)
			args = append(args, *req.IsActive)
			argPos++
		}
		query += fmt.Sprintf(" WHERE code = $%d", argPos)
		args = append(args, code)
		query += " RETURNING code, name_cn, description, COALESCE(is_active, true), COALESCE(created_at, NOW()), COALESCE(updated_at, created_at, NOW())"

		var roleCode, name string
		var desc *string
		var active bool
		var createdAt, updatedAt time.Time
		if err := s.pool.QueryRow(ctx, query, args...).Scan(&roleCode, &name, &desc, &active, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to update legacy role: %w", err)
		}

		r := &InstitutionRole{
			ID:          legacyInstitutionRoleID(roleCode),
			TenantID:    tenantID,
			Name:        name,
			Code:        roleCode,
			Description: desc,
			IsActive:    active,
		}
		r.CreatedAt = createdAt
		r.UpdatedAt = updatedAt
		return r, nil
	}

	query := "UPDATE institution_roles SET updated_at = NOW()"
	args := []interface{}{}
	argPos := 1

	if req.Name != nil {
		query += fmt.Sprintf(", name = $%d", argPos)
		args = append(args, *req.Name)
		argPos++
	}

	if req.Description != nil {
		query += fmt.Sprintf(", description = $%d", argPos)
		args = append(args, *req.Description)
		argPos++
	}

	if req.IsActive != nil {
		query += fmt.Sprintf(", is_active = $%d", argPos)
		args = append(args, *req.IsActive)
		argPos++
	}

	query += fmt.Sprintf(" WHERE id = $%d AND tenant_id = $%d AND deleted_at IS NULL", argPos, argPos+1)
	args = append(args, id, tenantID)
	query += " RETURNING id, tenant_id, name, code, description, is_active, created_at, updated_at"

	var r InstitutionRole
	err = s.pool.QueryRow(ctx, query, args...).Scan(
		&r.ID, &r.TenantID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update role: %w", err)
	}

	return &r, nil
}

// DeleteInstitutionRole deletes an institution role (soft delete)
func (s *Store) DeleteInstitutionRole(ctx context.Context, tenantID, id int64) error {
	hasNewTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return err
	}
	if !hasNewTable {
		code, err := s.findLegacyRoleCodeByID(ctx, id)
		if err != nil {
			return fmt.Errorf("legacy role not found: %w", err)
		}
		result, err := s.pool.Exec(ctx, "DELETE FROM inst_roles WHERE code = $1", code)
		if err != nil {
			return fmt.Errorf("failed to delete legacy role: %w", err)
		}
		if result.RowsAffected() == 0 {
			return fmt.Errorf("role not found")
		}
		return nil
	}

	query := `
		UPDATE institution_roles
		SET deleted_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete role: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("role not found")
	}

	return nil
}

// ListInstitutionMenus retrieves all institution menus
func (s *Store) ListInstitutionMenus(ctx context.Context, tenantID *int64, req InstitutionMenuListRequest) ([]*InstitutionMenu, error) {
	institutionMenusExists, err := s.hasInstitutionMenusTable(ctx)
	if err != nil {
		return nil, err
	}

	if !institutionMenusExists {
		query := `
			SELECT id, NULL::bigint AS tenant_id, name, code, path, icon, parent_id,
			       COALESCE(order_index, 0) AS sort_order,
			       COALESCE(is_active, true) AS is_active,
			       COALESCE(is_feature_assignable, false) AS is_feature_assignable,
			       COALESCE(is_default_for_admin, false) AS is_default_for_admin,
			       NULLIF(feature_code, '') AS feature_code,
			       NULLIF(feature_name, '') AS feature_name,
			       created_at,
			       COALESCE(created_at, NOW()) AS updated_at
			FROM inst_menus
			WHERE 1 = 1
		`
		args := []interface{}{}
		argPos := 1

		if req.Name != "" {
			query += fmt.Sprintf(" AND name ILIKE $%d", argPos)
			args = append(args, "%"+req.Name+"%")
			argPos++
		}
		if req.IsActive != nil {
			query += fmt.Sprintf(" AND COALESCE(is_active, true) = $%d", argPos)
			args = append(args, *req.IsActive)
			argPos++
		}
		query += " ORDER BY COALESCE(order_index, 0), id"

		rows, err := s.pool.Query(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to query inst menus: %w", err)
		}
		defer rows.Close()

		var menus []*InstitutionMenu
		for rows.Next() {
			var m InstitutionMenu
			if err := rows.Scan(&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.IsFeatureAssignable, &m.IsDefaultForAdmin, &m.FeatureCode, &m.FeatureName, &m.CreatedAt, &m.UpdatedAt); err != nil {
				return nil, fmt.Errorf("failed to scan inst menu: %w", err)
			}
			menus = append(menus, &m)
		}
		return menus, nil
	}

	query := `
		SELECT id, tenant_id, name, code, path, icon, parent_id, sort_order, is_active,
		       COALESCE(is_feature_assignable, false) AS is_feature_assignable,
		       COALESCE(is_default_for_admin, false) AS is_default_for_admin,
		       NULLIF(feature_code, '') AS feature_code,
		       NULLIF(feature_name, '') AS feature_name,
		       created_at, updated_at
		FROM institution_menus
		WHERE deleted_at IS NULL
	`
	args := []interface{}{}
	argPos := 1

	if tenantID != nil {
		query += fmt.Sprintf(" AND (tenant_id = $%d OR tenant_id IS NULL)", argPos)
		args = append(args, *tenantID)
		argPos++
	} else {
		query += " AND tenant_id IS NULL"
	}

	if req.Name != "" {
		query += fmt.Sprintf(" AND name ILIKE $%d", argPos)
		args = append(args, "%"+req.Name+"%")
		argPos++
	}

	if req.IsActive != nil {
		query += fmt.Sprintf(" AND is_active = $%d", argPos)
		args = append(args, *req.IsActive)
		argPos++
	}

	query += " ORDER BY sort_order, created_at"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query menus: %w", err)
	}
	defer rows.Close()

	var menus []*InstitutionMenu
	for rows.Next() {
		var m InstitutionMenu
		err := rows.Scan(&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.IsFeatureAssignable, &m.IsDefaultForAdmin, &m.FeatureCode, &m.FeatureName, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan menu: %w", err)
		}
		menus = append(menus, &m)
	}

	return menus, nil
}

// GetTenantAllowedMenuCodes returns whether tenant is unrestricted and explicit allowed menu codes.
func (s *Store) GetTenantAllowedMenuCodes(ctx context.Context, tenantID int64) (bool, map[string]struct{}, error) {
	var groupID *int64
	err := s.pool.QueryRow(ctx, `
		SELECT g.id
		FROM tenant_feature_assignments a
		JOIN tenant_feature_groups g ON g.id = a.group_id
		WHERE a.tenant_id = $1
		  AND CASE
		      WHEN g.is_active::text IN ('1','t','true','TRUE') THEN true
		      ELSE false
		  END = true
	`, tenantID).Scan(&groupID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, nil, fmt.Errorf("failed to query tenant feature assignment: %w", err)
	}

	if groupID == nil {
		return false, map[string]struct{}{}, nil
	}

	menuDefs, err := s.listTenantMenuDefinitions(ctx, tenantID)
	if err != nil {
		return false, nil, err
	}

	items := make([]menuaccess.PolicyItem, 0)
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(item_type, ''), 'feature') AS item_type,
		       COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')) AS item_code,
		       COALESCE(is_enabled, true) AS is_enabled
		FROM tenant_feature_group_items
		WHERE group_id = $1
	`, *groupID)
	if err != nil {
		return false, nil, fmt.Errorf("failed to query feature group menu items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var itemType, code string
		var enabled bool
		if err := rows.Scan(&itemType, &code, &enabled); err != nil {
			return false, nil, fmt.Errorf("failed to scan feature group menu item: %w", err)
		}
		items = append(items, menuaccess.PolicyItem{ItemType: itemType, ItemCode: code, Enabled: enabled})
	}

	overrides := make([]menuaccess.PolicyOverride, 0)
	overrideRows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(item_type, ''), 'feature') AS item_type,
		       COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')) AS item_code,
		       COALESCE(NULLIF(override_mode, ''), CASE WHEN COALESCE(is_enabled, true) THEN 'allow' ELSE 'deny' END) AS override_mode
		FROM tenant_feature_overrides
		WHERE tenant_id = $1
	`, tenantID)
	if err != nil {
		return false, nil, fmt.Errorf("failed to query feature overrides: %w", err)
	}
	defer overrideRows.Close()
	for overrideRows.Next() {
		var itemType, code, mode string
		if err := overrideRows.Scan(&itemType, &code, &mode); err != nil {
			return false, nil, fmt.Errorf("failed to scan feature override: %w", err)
		}
		overrides = append(overrides, menuaccess.PolicyOverride{ItemType: itemType, ItemCode: code, OverrideMode: mode})
	}
	return false, menuaccess.ResolveAllowedMenuCodes(menuDefs, items, overrides), nil
}

func (s *Store) listTenantMenuDefinitions(ctx context.Context, tenantID int64) ([]menuaccess.MenuDefinition, error) {
	institutionMenusExists, err := s.hasInstitutionMenusTable(ctx)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT lower(trim(code)) AS code,
		       lower(trim(COALESCE(feature_code, ''))) AS feature_code
		FROM inst_menus
		WHERE COALESCE(is_active, true) = true
	`
	args := []interface{}{}
	if institutionMenusExists {
		query = `
			SELECT lower(trim(code)) AS code,
			       lower(trim(COALESCE(feature_code, ''))) AS feature_code
			FROM institution_menus
			WHERE deleted_at IS NULL
			  AND COALESCE(is_active, true) = true
			  AND (tenant_id = $1 OR tenant_id IS NULL)
		`
		args = append(args, tenantID)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tenant menu definitions: %w", err)
	}
	defer rows.Close()

	defs := make([]menuaccess.MenuDefinition, 0)
	for rows.Next() {
		var def menuaccess.MenuDefinition
		if err := rows.Scan(&def.Code, &def.FeatureCode); err != nil {
			return nil, fmt.Errorf("failed to scan tenant menu definition: %w", err)
		}
		defs = append(defs, def)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate tenant menu definitions: %w", err)
	}
	return defs, nil
}

// GetInstitutionMenuByID retrieves an institution menu by ID
func (s *Store) GetInstitutionMenuByID(ctx context.Context, tenantID *int64, id int64) (*InstitutionMenu, error) {
	institutionMenusExists, err := s.hasInstitutionMenusTable(ctx)
	if err != nil {
		return nil, err
	}
	if !institutionMenusExists {
		query := `
			SELECT id, NULL::bigint AS tenant_id, name, code, path, icon, parent_id,
			       COALESCE(order_index, 0) AS sort_order,
			       COALESCE(is_active, true) AS is_active,
			       COALESCE(is_feature_assignable, false) AS is_feature_assignable,
			       COALESCE(is_default_for_admin, false) AS is_default_for_admin,
			       NULLIF(feature_code, '') AS feature_code,
			       NULLIF(feature_name, '') AS feature_name,
			       COALESCE(created_at, NOW()) AS created_at,
			       COALESCE(created_at, NOW()) AS updated_at
			FROM inst_menus
			WHERE id = $1
		`
		var m InstitutionMenu
		if err := s.pool.QueryRow(ctx, query, id).Scan(
			&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.IsFeatureAssignable, &m.IsDefaultForAdmin, &m.FeatureCode, &m.FeatureName, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("legacy menu not found: %w", err)
		}
		return &m, nil
	}

	query := `
		SELECT id, tenant_id, name, code, path, icon, parent_id, sort_order, is_active,
		       COALESCE(is_feature_assignable, false) AS is_feature_assignable,
		       COALESCE(is_default_for_admin, false) AS is_default_for_admin,
		       NULLIF(feature_code, '') AS feature_code,
		       NULLIF(feature_name, '') AS feature_name,
		       created_at, updated_at
		FROM institution_menus
		WHERE id = $1 AND deleted_at IS NULL
	`
	args := []interface{}{id}

	if tenantID != nil {
		query += " AND (tenant_id = $2 OR tenant_id IS NULL)"
		args = append(args, *tenantID)
	} else {
		query += " AND tenant_id IS NULL"
	}

	var m InstitutionMenu
	err = s.pool.QueryRow(ctx, query, args...).Scan(
		&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.IsFeatureAssignable, &m.IsDefaultForAdmin, &m.FeatureCode, &m.FeatureName, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("menu not found: %w", err)
	}

	return &m, nil
}

// CreateInstitutionMenu creates a new institution menu
func (s *Store) CreateInstitutionMenu(ctx context.Context, tenantID *int64, req CreateInstitutionMenuRequest) (*InstitutionMenu, error) {
	institutionMenusExists, err := s.hasInstitutionMenusTable(ctx)
	if err != nil {
		return nil, err
	}
	featureAssignable := req.IsFeatureAssignable
	if !featureAssignable {
		code := strings.ToLower(strings.TrimSpace(req.Code))
		switch code {
		case "trial_home", "recording_upload":
			featureAssignable = true
		}
	}
	if !institutionMenusExists {
		query := `
			INSERT INTO inst_menus (code, name, path, icon, parent_id, order_index, is_active, is_feature_assignable, is_default_for_admin, feature_code, feature_name, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, true, $7, $8, NULLIF($9, ''), NULLIF($10, ''), NOW())
			RETURNING id, NULL::bigint AS tenant_id, name, code, path, icon, parent_id,
			          COALESCE(order_index, 0) AS sort_order,
			          COALESCE(is_active, true) AS is_active,
			          COALESCE(is_feature_assignable, false) AS is_feature_assignable,
			          COALESCE(is_default_for_admin, false) AS is_default_for_admin,
			          NULLIF(feature_code, '') AS feature_code,
			          NULLIF(feature_name, '') AS feature_name,
			          COALESCE(created_at, NOW()) AS created_at,
			          COALESCE(created_at, NOW()) AS updated_at
		`
		var m InstitutionMenu
		if err := s.pool.QueryRow(ctx, query, req.Code, req.Name, req.Path, req.Icon, req.ParentID, req.SortOrder, featureAssignable, req.IsDefaultForAdmin, stringValue(req.FeatureCode), stringValue(req.FeatureName)).Scan(
			&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.IsFeatureAssignable, &m.IsDefaultForAdmin, &m.FeatureCode, &m.FeatureName, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to create legacy menu: %w", err)
		}
		return &m, nil
	}

	query := `
		INSERT INTO institution_menus (tenant_id, name, code, path, icon, parent_id, sort_order, is_active, is_feature_assignable, is_default_for_admin, feature_code, feature_name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, true, $8, $9, NULLIF($10, ''), NULLIF($11, ''), NOW(), NOW())
		RETURNING id, tenant_id, name, code, path, icon, parent_id, sort_order, is_active,
		          COALESCE(is_feature_assignable, false), COALESCE(is_default_for_admin, false), NULLIF(feature_code, ''), NULLIF(feature_name, ''), created_at, updated_at
	`

	var m InstitutionMenu
	err = s.pool.QueryRow(ctx, query, tenantID, req.Name, req.Code, req.Path, req.Icon, req.ParentID, req.SortOrder, featureAssignable, req.IsDefaultForAdmin, stringValue(req.FeatureCode), stringValue(req.FeatureName)).Scan(
		&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.IsFeatureAssignable, &m.IsDefaultForAdmin, &m.FeatureCode, &m.FeatureName, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create menu: %w", err)
	}

	return &m, nil
}

// UpdateInstitutionMenu updates an institution menu
func (s *Store) UpdateInstitutionMenu(ctx context.Context, tenantID *int64, id int64, req UpdateInstitutionMenuRequest) (*InstitutionMenu, error) {
	institutionMenusExists, err := s.hasInstitutionMenusTable(ctx)
	if err != nil {
		return nil, err
	}
	if !institutionMenusExists {
		query := "UPDATE inst_menus SET"
		updates := make([]string, 0, 10)
		args := []interface{}{}
		argPos := 1

		if req.Name != nil {
			updates = append(updates, fmt.Sprintf(" name = $%d", argPos))
			args = append(args, *req.Name)
			argPos++
		}
		if req.Path != nil {
			updates = append(updates, fmt.Sprintf(" path = $%d", argPos))
			args = append(args, *req.Path)
			argPos++
		}
		if req.Icon != nil {
			updates = append(updates, fmt.Sprintf(" icon = $%d", argPos))
			args = append(args, *req.Icon)
			argPos++
		}
		if req.ParentID != nil {
			updates = append(updates, fmt.Sprintf(" parent_id = $%d", argPos))
			args = append(args, *req.ParentID)
			argPos++
		}
		if req.SortOrder != nil {
			updates = append(updates, fmt.Sprintf(" order_index = $%d", argPos))
			args = append(args, *req.SortOrder)
			argPos++
		}
		if req.IsActive != nil {
			updates = append(updates, fmt.Sprintf(" is_active = $%d", argPos))
			args = append(args, *req.IsActive)
			argPos++
		}
		if req.IsFeatureAssignable != nil {
			updates = append(updates, fmt.Sprintf(" is_feature_assignable = $%d", argPos))
			args = append(args, *req.IsFeatureAssignable)
			argPos++
		}
		if req.IsDefaultForAdmin != nil {
			updates = append(updates, fmt.Sprintf(" is_default_for_admin = $%d", argPos))
			args = append(args, *req.IsDefaultForAdmin)
			argPos++
		}
		if req.FeatureCode != nil {
			updates = append(updates, fmt.Sprintf(" feature_code = NULLIF($%d, '')", argPos))
			args = append(args, *req.FeatureCode)
			argPos++
		}
		if req.FeatureName != nil {
			updates = append(updates, fmt.Sprintf(" feature_name = NULLIF($%d, '')", argPos))
			args = append(args, *req.FeatureName)
			argPos++
		}
		if len(updates) == 0 {
			return s.GetInstitutionMenuByID(ctx, tenantID, id)
		}

		query += fmt.Sprintf("%s WHERE id = $%d RETURNING id, NULL::bigint AS tenant_id, name, code, path, icon, parent_id, COALESCE(order_index, 0), COALESCE(is_active, true), COALESCE(is_feature_assignable, false), COALESCE(is_default_for_admin, false), NULLIF(feature_code, ''), NULLIF(feature_name, ''), COALESCE(created_at, NOW()), COALESCE(created_at, NOW())", strings.Join(updates, ","), argPos)
		args = append(args, id)

		var m InstitutionMenu
		if err := s.pool.QueryRow(ctx, query, args...).Scan(
			&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.IsFeatureAssignable, &m.IsDefaultForAdmin, &m.FeatureCode, &m.FeatureName, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to update legacy menu: %w", err)
		}
		return &m, nil
	}

	query := "UPDATE institution_menus SET updated_at = NOW()"
	args := []interface{}{}
	argPos := 1

	if req.Name != nil {
		query += fmt.Sprintf(", name = $%d", argPos)
		args = append(args, *req.Name)
		argPos++
	}

	if req.Path != nil {
		query += fmt.Sprintf(", path = $%d", argPos)
		args = append(args, *req.Path)
		argPos++
	}

	if req.Icon != nil {
		query += fmt.Sprintf(", icon = $%d", argPos)
		args = append(args, *req.Icon)
		argPos++
	}

	if req.ParentID != nil {
		query += fmt.Sprintf(", parent_id = $%d", argPos)
		args = append(args, *req.ParentID)
		argPos++
	}

	if req.SortOrder != nil {
		query += fmt.Sprintf(", sort_order = $%d", argPos)
		args = append(args, *req.SortOrder)
		argPos++
	}

	if req.IsActive != nil {
		query += fmt.Sprintf(", is_active = $%d", argPos)
		args = append(args, *req.IsActive)
		argPos++
	}
	if req.IsFeatureAssignable != nil {
		query += fmt.Sprintf(", is_feature_assignable = $%d", argPos)
		args = append(args, *req.IsFeatureAssignable)
		argPos++
	}
	if req.IsDefaultForAdmin != nil {
		query += fmt.Sprintf(", is_default_for_admin = $%d", argPos)
		args = append(args, *req.IsDefaultForAdmin)
		argPos++
	}
	if req.FeatureCode != nil {
		query += fmt.Sprintf(", feature_code = NULLIF($%d, '')", argPos)
		args = append(args, *req.FeatureCode)
		argPos++
	}
	if req.FeatureName != nil {
		query += fmt.Sprintf(", feature_name = NULLIF($%d, '')", argPos)
		args = append(args, *req.FeatureName)
		argPos++
	}

	query += fmt.Sprintf(" WHERE id = $%d AND deleted_at IS NULL", argPos)
	args = append(args, id)

	if tenantID != nil {
		query += fmt.Sprintf(" AND (tenant_id = $%d OR tenant_id IS NULL)", argPos+1)
		args = append(args, *tenantID)
	} else {
		query += " AND tenant_id IS NULL"
	}

	query += " RETURNING id, tenant_id, name, code, path, icon, parent_id, sort_order, is_active, COALESCE(is_feature_assignable, false), COALESCE(is_default_for_admin, false), NULLIF(feature_code, ''), NULLIF(feature_name, ''), created_at, updated_at"

	var m InstitutionMenu
	err = s.pool.QueryRow(ctx, query, args...).Scan(
		&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.IsFeatureAssignable, &m.IsDefaultForAdmin, &m.FeatureCode, &m.FeatureName, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update menu: %w", err)
	}

	return &m, nil
}

// DeleteInstitutionMenu deletes an institution menu (soft delete)
func (s *Store) DeleteInstitutionMenu(ctx context.Context, tenantID *int64, id int64) error {
	institutionMenusExists, err := s.hasInstitutionMenusTable(ctx)
	if err != nil {
		return err
	}
	if !institutionMenusExists {
		result, err := s.pool.Exec(ctx, "DELETE FROM inst_menus WHERE id = $1", id)
		if err != nil {
			return fmt.Errorf("failed to delete legacy menu: %w", err)
		}
		if result.RowsAffected() == 0 {
			return fmt.Errorf("menu not found")
		}
		return nil
	}

	query := `
		UPDATE institution_menus
		SET deleted_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`
	args := []interface{}{id}

	if tenantID != nil {
		query += " AND (tenant_id = $2 OR tenant_id IS NULL)"
		args = append(args, *tenantID)
	} else {
		query += " AND tenant_id IS NULL"
	}

	result, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete menu: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("menu not found")
	}

	return nil
}

// AssignPermissionsToInstitutionRole assigns permissions to an institution role
func (s *Store) AssignPermissionsToInstitutionRole(ctx context.Context, tenantID, roleID int64, permissionIDs []int64) error {
	hasNewTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return err
	}
	var roleCode string
	if hasNewTable {
		if err := s.pool.QueryRow(ctx, `
			SELECT code FROM institution_roles
			WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`, roleID, tenantID).Scan(&roleCode); err != nil {
			return fmt.Errorf("role not found: %w", err)
		}
	} else {
		roleCode, err = s.findLegacyRoleCodeByID(ctx, roleID)
		if err != nil {
			return fmt.Errorf("legacy role not found: %w", err)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	hasInstitutionRoleMenus, err := s.hasInstitutionRoleMenusTable(ctx)
	if err != nil {
		return err
	}
	institutionMenusExists, err := s.hasInstitutionMenusTable(ctx)
	if err != nil {
		return err
	}
	if !hasInstitutionRoleMenus || !institutionMenusExists {
		return fmt.Errorf("institution role menu tables not prepared")
	}

	menuCodes, err := s.resolveMenuCodesByIDs(ctx, tenantID, permissionIDs)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM institution_role_menus WHERE tenant_id = $1 AND role_code = $2", tenantID, roleCode); err != nil {
		return fmt.Errorf("failed to delete existing institution role menus: %w", err)
	}
	for _, menuCode := range menuCodes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO institution_role_menus (tenant_id, role_code, menu_code, created_at, updated_at)
			VALUES ($1, $2, $3, NOW(), NOW())
		`, tenantID, roleCode, menuCode); err != nil {
			return fmt.Errorf("failed to assign institution role menu: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// RemovePermissionsFromInstitutionRole removes permissions from an institution role
func (s *Store) RemovePermissionsFromInstitutionRole(ctx context.Context, tenantID, roleID int64, permissionIDs []int64) error {
	hasNewTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return err
	}
	var roleCode string
	if hasNewTable {
		if err := s.pool.QueryRow(ctx, `
			SELECT code FROM institution_roles
			WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`, roleID, tenantID).Scan(&roleCode); err != nil {
			return fmt.Errorf("role not found: %w", err)
		}
	} else {
		roleCode, err = s.findLegacyRoleCodeByID(ctx, roleID)
		if err != nil {
			return fmt.Errorf("legacy role not found: %w", err)
		}
	}

	hasInstitutionRoleMenus, err := s.hasInstitutionRoleMenusTable(ctx)
	if err != nil {
		return err
	}
	institutionMenusExists, err := s.hasInstitutionMenusTable(ctx)
	if err != nil {
		return err
	}
	if !hasInstitutionRoleMenus || !institutionMenusExists {
		return fmt.Errorf("institution role menu tables not prepared")
	}

	menuCodes, err := s.resolveMenuCodesByIDs(ctx, tenantID, permissionIDs)
	if err != nil {
		return err
	}
	if len(menuCodes) == 0 {
		return nil
	}
	_, err = s.pool.Exec(ctx, `
		DELETE FROM institution_role_menus
		WHERE tenant_id = $1 AND role_code = $2 AND menu_code = ANY($3)
	`, tenantID, roleCode, menuCodes)
	if err != nil {
		return fmt.Errorf("failed to remove institution role menus: %w", err)
	}

	return nil
}

// GetInstitutionRolePermissions retrieves permissions for an institution role
func (s *Store) GetInstitutionRolePermissions(ctx context.Context, tenantID, roleID int64) ([]*InstitutionPermission, error) {
	hasNewTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return nil, err
	}
	var roleCode string
	if hasNewTable {
		if err := s.pool.QueryRow(ctx, `
			SELECT code FROM institution_roles
			WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`, roleID, tenantID).Scan(&roleCode); err != nil {
			return nil, fmt.Errorf("role not found: %w", err)
		}
	} else {
		roleCode, err = s.findLegacyRoleCodeByID(ctx, roleID)
		if err != nil {
			return nil, fmt.Errorf("legacy role not found: %w", err)
		}
	}

	hasInstitutionRoleMenus, err := s.hasInstitutionRoleMenusTable(ctx)
	if err != nil {
		return nil, err
	}
	institutionMenusExists, err := s.hasInstitutionMenusTable(ctx)
	if err != nil {
		return nil, err
	}
	if !hasInstitutionRoleMenus || !institutionMenusExists {
		return nil, fmt.Errorf("institution role menu tables not prepared")
	}
	query := `
		SELECT m.id,
		       m.name,
		       m.code,
		       'menu'::varchar AS resource,
		       'view'::varchar AS action,
		       m.path::varchar AS description,
		       COALESCE(m.created_at, NOW()) AS created_at,
		       COALESCE(m.updated_at, m.created_at, NOW()) AS updated_at
		FROM institution_menus m
		JOIN institution_role_menus rm ON rm.menu_code = m.code
		WHERE rm.tenant_id = $1
		  AND rm.role_code = $2
		  AND m.deleted_at IS NULL
		  AND (m.tenant_id = $1 OR m.tenant_id IS NULL)
		ORDER BY COALESCE(m.sort_order, 0), m.id
	`
	args := []interface{}{tenantID, roleCode}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query role menus: %w", err)
	}
	defer rows.Close()

	var permissions []*InstitutionPermission
	for rows.Next() {
		var p InstitutionPermission
		err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.Resource, &p.Action, &p.Description, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan permission: %w", err)
		}
		permissions = append(permissions, &p)
	}

	return permissions, nil
}

// GetEmployeeRole retrieves the role for an employee
func (s *Store) GetEmployeeRole(ctx context.Context, employeeID int64) (*InstitutionRole, error) {
	tenantID, err := s.getEmployeeTenantID(ctx, employeeID)
	if err != nil {
		return nil, err
	}

	hasNewRoleTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return nil, err
	}
	hasInstitutionEmployeeRoles, err := s.hasInstitutionEmployeeRolesTable(ctx)
	if err != nil {
		return nil, err
	}
	if !hasInstitutionEmployeeRoles {
		return nil, fmt.Errorf("institution employee role table not prepared")
	}

	var query string
	if hasNewRoleTable {
		query = `
			SELECT COALESCE(ir.id, 0) AS role_id,
			       er.tenant_id,
			       COALESCE(NULLIF(ir.name, ''), NULLIF(r.name_cn, ''), er.role_code) AS role_name,
			       er.role_code,
			       ir.description,
			       COALESCE(ir.is_active, true) AS is_active,
			       COALESCE(ir.created_at, er.created_at, NOW()) AS created_at,
			       COALESCE(ir.updated_at, ir.created_at, er.created_at, NOW()) AS updated_at
			FROM institution_employee_roles er
			LEFT JOIN institution_roles ir
			  ON ir.tenant_id = er.tenant_id
			 AND lower(ir.code) = lower(er.role_code)
			 AND ir.deleted_at IS NULL
			LEFT JOIN inst_roles r ON lower(r.code) = lower(er.role_code)
			WHERE er.tenant_id = $1 AND er.employee_id = $2
			  AND trim(COALESCE(er.role_code, '')) <> ''
			ORDER BY er.updated_at DESC NULLS LAST, er.created_at DESC, er.id DESC
			LIMIT 1
		`
	} else {
		query = `
			SELECT 0 AS role_id,
			       er.tenant_id,
			       COALESCE(NULLIF(r.name_cn, ''), er.role_code) AS role_name,
			       er.role_code,
			       NULL::text AS description,
			       true AS is_active,
			       COALESCE(er.created_at, NOW()) AS created_at,
			       COALESCE(er.created_at, NOW()) AS updated_at
			FROM institution_employee_roles er
			LEFT JOIN inst_roles r ON lower(r.code) = lower(er.role_code)
			WHERE er.tenant_id = $1 AND er.employee_id = $2
			  AND trim(COALESCE(er.role_code, '')) <> ''
			ORDER BY er.updated_at DESC NULLS LAST, er.created_at DESC, er.id DESC
			LIMIT 1
		`
	}

	var role InstitutionRole
	if err := s.pool.QueryRow(ctx, query, tenantID, employeeID).Scan(
		&role.ID,
		&role.TenantID,
		&role.Name,
		&role.Code,
		&role.Description,
		&role.IsActive,
		&role.CreatedAt,
		&role.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("employee role not found: %w", err)
	}
	role.Code = normalizeInstitutionRoleCode(role.Code)
	if role.ID <= 0 {
		role.ID = legacyInstitutionRoleID(role.Code)
	}
	return &role, nil
}

// SetEmployeeRole sets the role for an employee
func (s *Store) SetEmployeeRole(ctx context.Context, employeeID, roleID int64) error {
	hasNewRoleTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return err
	}
	hasInstitutionEmployeeRoleTable, err := s.hasInstitutionEmployeeRolesTable(ctx)
	if err != nil {
		return err
	}

	var tenantID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT tenant_id FROM employees WHERE id = $1 AND deleted_at IS NULL
	`, employeeID).Scan(&tenantID); err != nil {
		return fmt.Errorf("employee not found: %w", err)
	}

	var roleCode string
	var institutionRoleID *int64
	if hasNewRoleTable {
		var resolvedRoleID int64
		if err := s.pool.QueryRow(ctx, `
			SELECT id, code
			FROM institution_roles
			WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`, roleID, tenantID).Scan(&resolvedRoleID, &roleCode); err != nil {
			return fmt.Errorf("role not found: %w", err)
		}
		institutionRoleID = &resolvedRoleID
	} else {
		var err error
		roleCode, err = s.findLegacyRoleCodeByID(ctx, roleID)
		if err != nil {
			return fmt.Errorf("role not found: %w", err)
		}
	}
	roleCode = normalizeInstitutionRoleCode(roleCode)
	if isHiddenInstitutionRoleCode(roleCode) {
		return fmt.Errorf("role %s is not assignable to institution employees", roleCode)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if hasInstitutionEmployeeRoleTable {
		if err := s.upsertInstitutionEmployeeRoleTx(ctx, tx, tenantID, employeeID, institutionRoleID, roleCode, "manual"); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	return fmt.Errorf("institution employee role table not prepared")
}

// RemoveEmployeeRole removes the role from an employee
func (s *Store) RemoveEmployeeRole(ctx context.Context, employeeID int64) error {
	var tenantID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT tenant_id FROM employees WHERE id = $1 AND deleted_at IS NULL
	`, employeeID).Scan(&tenantID); err != nil {
		return fmt.Errorf("employee not found: %w", err)
	}
	hasInstitutionEmployeeRoleTable, err := s.hasInstitutionEmployeeRolesTable(ctx)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if hasInstitutionEmployeeRoleTable {
		if err := s.removeInstitutionEmployeeRoleTx(ctx, tx, tenantID, employeeID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	return fmt.Errorf("institution employee role table not prepared")
}

// GetDepartmentRole retrieves the default role for a department.
func (s *Store) GetDepartmentRole(ctx context.Context, departmentID int64) (*InstitutionRole, error) {
	var tenantID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT tenant_id FROM departments WHERE id = $1 AND deleted_at IS NULL
	`, departmentID).Scan(&tenantID); err != nil {
		return nil, fmt.Errorf("department not found: %w", err)
	}

	hasNewRoleTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return nil, err
	}
	hasNewDepartmentRoleTable, err := s.hasInstitutionDepartmentRolesTable(ctx)
	if err != nil {
		return nil, err
	}
	if hasNewDepartmentRoleTable {
		var r InstitutionRole
		if hasNewRoleTable {
			if err := s.pool.QueryRow(ctx, `
				SELECT COALESCE(ir.id, 0) AS role_id,
				       dr.tenant_id,
				       COALESCE(NULLIF(ir.name, ''), COALESCE(NULLIF(TRIM(dr.role_code), ''), '')) AS role_name,
				       COALESCE(NULLIF(TRIM(dr.role_code), ''), COALESCE(ir.code, '')) AS role_code,
				       ir.description,
				       COALESCE(ir.is_active, true) AS is_active,
				       COALESCE(ir.created_at, dr.created_at, NOW()) AS created_at,
				       COALESCE(ir.updated_at, dr.updated_at, dr.created_at, NOW()) AS updated_at
				FROM institution_department_roles dr
				LEFT JOIN LATERAL (
					SELECT id, tenant_id, name, code, description, is_active, created_at, updated_at
					FROM institution_roles ir
					WHERE ir.tenant_id = dr.tenant_id
					  AND ir.deleted_at IS NULL
					  AND (
						(dr.role_id IS NOT NULL AND ir.id = dr.role_id)
						OR (
							NULLIF(TRIM(dr.role_code), '') IS NOT NULL
							AND lower(ir.code) = lower(TRIM(dr.role_code))
						)
					  )
					ORDER BY CASE WHEN dr.role_id IS NOT NULL AND ir.id = dr.role_id THEN 0 ELSE 1 END, ir.id DESC
					LIMIT 1
				) ir ON true
				WHERE dr.department_id = $1
				ORDER BY COALESCE(dr.updated_at, dr.created_at) DESC
				LIMIT 1
			`, departmentID).Scan(&r.ID, &r.TenantID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt); err == nil {
				r.Code = normalizeInstitutionRoleCode(r.Code)
				if r.ID <= 0 {
					r.ID = legacyInstitutionRoleID(r.Code)
				}
				return &r, nil
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("failed to query institution department role: %w", err)
			}
		}
	}

	query := `
		SELECT dr.role_code,
		       COALESCE(NULLIF(r.name_cn, ''), dr.role_code) AS role_name
		FROM inst_department_roles dr
		LEFT JOIN inst_roles r ON r.code = dr.role_code
		WHERE dr.tenant_id = $1 AND dr.department_id = $2
		ORDER BY dr.created_at DESC
		LIMIT 1
	`
	var roleCode, roleName string
	if err := s.pool.QueryRow(ctx, query, tenantID, departmentID).Scan(&roleCode, &roleName); err != nil {
		return nil, fmt.Errorf("department role not found: %w", err)
	}
	roleCode = normalizeInstitutionRoleCode(roleCode)

	now := time.Now()
	return &InstitutionRole{
		ID:          legacyInstitutionRoleID(roleCode),
		TenantID:    tenantID,
		Name:        roleName,
		Code:        roleCode,
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
		Description: nil,
	}, nil
}

// SetDepartmentRole sets the default role for a department.
func (s *Store) SetDepartmentRole(ctx context.Context, departmentID int64, req SetDepartmentRoleRequest) error {
	var tenantID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT tenant_id FROM departments WHERE id = $1 AND deleted_at IS NULL
	`, departmentID).Scan(&tenantID); err != nil {
		return fmt.Errorf("department not found: %w", err)
	}

	hasNewRoleTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return err
	}
	hasNewDepartmentRoleTable, err := s.hasInstitutionDepartmentRolesTable(ctx)
	if err != nil {
		return err
	}

	if hasNewRoleTable && hasNewDepartmentRoleTable {
		var roleID int64
		roleCode := ""
		if req.RoleID != nil && *req.RoleID > 0 {
			if err := s.pool.QueryRow(ctx, `
				SELECT id, code
				FROM institution_roles
				WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
			`, *req.RoleID, tenantID).Scan(&roleID, &roleCode); err != nil {
				return fmt.Errorf("role not found: %w", err)
			}
		} else if req.RoleCode != nil && strings.TrimSpace(*req.RoleCode) != "" {
			if err := s.pool.QueryRow(ctx, `
				SELECT id, code
				FROM institution_roles
				WHERE lower(code) = lower($1) AND tenant_id = $2 AND deleted_at IS NULL
			`, strings.TrimSpace(*req.RoleCode), tenantID).Scan(&roleID, &roleCode); err != nil {
				return fmt.Errorf("role not found: %w", err)
			}
		} else {
			return fmt.Errorf("role_id or role_code is required")
		}
		roleCode = normalizeInstitutionRoleCode(roleCode)
		if isHiddenInstitutionRoleCode(roleCode) {
			return fmt.Errorf("role %s is not assignable to institution departments", roleCode)
		}

		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback(ctx)

		if _, err := tx.Exec(ctx, `DELETE FROM institution_department_roles WHERE department_id = $1`, departmentID); err != nil {
			return fmt.Errorf("failed to clear existing department role: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO institution_department_roles (department_id, role_id, tenant_id, role_code, is_default, created_at, updated_at)
			VALUES ($1, $2, $3, $4, true, NOW(), NOW())
		`, departmentID, roleID, tenantID, roleCode); err != nil {
			return fmt.Errorf("failed to set department role: %w", err)
		}

		if req.UpdateExisting {
			if err := s.syncDepartmentEmployeeRolesTx(ctx, tx, tenantID, departmentID, &roleID, roleCode); err != nil {
				return err
			}
		}

		return tx.Commit(ctx)
	}

	roleCode := ""
	if req.RoleCode != nil {
		roleCode = strings.TrimSpace(*req.RoleCode)
	}
	if roleCode == "" && req.RoleID != nil {
		mapped, err := s.findLegacyRoleCodeByID(ctx, *req.RoleID)
		if err != nil {
			return fmt.Errorf("role not found: %w", err)
		}
		roleCode = mapped
	}
	roleCode = normalizeInstitutionRoleCode(roleCode)
	if roleCode == "" {
		return fmt.Errorf("role_code is required")
	}
	if isHiddenInstitutionRoleCode(roleCode) {
		return fmt.Errorf("role %s is not assignable to institution departments", roleCode)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM inst_department_roles
		WHERE tenant_id = $1 AND department_id = $2
	`, tenantID, departmentID); err != nil {
		return fmt.Errorf("failed to clear existing department role: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO inst_department_roles (tenant_id, department_id, role_code, is_default, created_at)
		VALUES ($1, $2, $3, true, NOW())
	`, tenantID, departmentID, roleCode); err != nil {
		return fmt.Errorf("failed to set department role: %w", err)
	}

	if req.UpdateExisting {
		if err := s.syncDepartmentEmployeeRolesTx(ctx, tx, tenantID, departmentID, nil, roleCode); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// RemoveDepartmentRole removes the default role from a department.
func (s *Store) RemoveDepartmentRole(ctx context.Context, departmentID int64) error {
	var tenantID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT tenant_id FROM departments WHERE id = $1 AND deleted_at IS NULL
	`, departmentID).Scan(&tenantID); err != nil {
		return fmt.Errorf("department not found: %w", err)
	}

	hasNewDepartmentRoleTable, err := s.hasInstitutionDepartmentRolesTable(ctx)
	if err != nil {
		return err
	}
	if hasNewDepartmentRoleTable {
		if _, err := s.pool.Exec(ctx, `DELETE FROM institution_department_roles WHERE department_id = $1`, departmentID); err != nil {
			return fmt.Errorf("failed to remove department role: %w", err)
		}
		return nil
	}

	if _, err := s.pool.Exec(ctx, `
		DELETE FROM inst_department_roles
		WHERE tenant_id = $1 AND department_id = $2
	`, tenantID, departmentID); err != nil {
		return fmt.Errorf("failed to remove department role: %w", err)
	}
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
