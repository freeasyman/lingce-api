package rbac

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

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

func (s *Store) hasInstitutionMenusTable(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('public.institution_menus') IS NOT NULL").Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to detect institution menu table: %w", err)
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
		if _, err := s.pool.Exec(ctx, "DELETE FROM inst_role_menus WHERE tenant_id = $1 AND role_code = $2", tenantID, code); err != nil {
			return fmt.Errorf("failed to clean legacy role menus: %w", err)
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
			if err := rows.Scan(&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt); err != nil {
				return nil, fmt.Errorf("failed to scan inst menu: %w", err)
			}
			menus = append(menus, &m)
		}
		return menus, nil
	}

	query := `
		SELECT id, tenant_id, name, code, path, icon, parent_id, sort_order, is_active, created_at, updated_at
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
		err := rows.Scan(&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan menu: %w", err)
		}
		menus = append(menus, &m)
	}

	return menus, nil
}

// GetTenantAllowedMenuCodes returns whether tenant is unrestricted and explicit allowed menu codes.
func (s *Store) GetTenantAllowedMenuCodes(ctx context.Context, tenantID int64) (bool, map[string]struct{}, error) {
	allowed := make(map[string]struct{})

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
		return true, allowed, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')) AS item_code,
		       COALESCE(is_enabled, true) AS is_enabled
		FROM tenant_feature_group_items
		WHERE group_id = $1
		  AND COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
	`, *groupID)
	if err != nil {
		return false, nil, fmt.Errorf("failed to query feature group menu items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		var enabled bool
		if err := rows.Scan(&code, &enabled); err != nil {
			return false, nil, fmt.Errorf("failed to scan feature group menu item: %w", err)
		}
		if enabled && code != "" {
			allowed[code] = struct{}{}
		}
	}

	overrideRows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')) AS item_code,
		       COALESCE(NULLIF(override_mode, ''), CASE WHEN COALESCE(is_enabled, true) THEN 'allow' ELSE 'deny' END) AS override_mode
		FROM tenant_feature_overrides
		WHERE tenant_id = $1
		  AND COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
	`, tenantID)
	if err != nil {
		return false, nil, fmt.Errorf("failed to query feature overrides: %w", err)
	}
	defer overrideRows.Close()
	for overrideRows.Next() {
		var code, mode string
		if err := overrideRows.Scan(&code, &mode); err != nil {
			return false, nil, fmt.Errorf("failed to scan feature override: %w", err)
		}
		if code == "" {
			continue
		}
		if mode == "allow" {
			allowed[code] = struct{}{}
		} else {
			delete(allowed, code)
		}
	}
	return false, allowed, nil
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
			       COALESCE(created_at, NOW()) AS created_at,
			       COALESCE(created_at, NOW()) AS updated_at
			FROM inst_menus
			WHERE id = $1
		`
		var m InstitutionMenu
		if err := s.pool.QueryRow(ctx, query, id).Scan(
			&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("legacy menu not found: %w", err)
		}
		return &m, nil
	}

	query := `
		SELECT id, tenant_id, name, code, path, icon, parent_id, sort_order, is_active, created_at, updated_at
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
		&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
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
	if !institutionMenusExists {
		query := `
			INSERT INTO inst_menus (code, name, path, icon, parent_id, order_index, is_active, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, true, NOW())
			RETURNING id, NULL::bigint AS tenant_id, name, code, path, icon, parent_id,
			          COALESCE(order_index, 0) AS sort_order,
			          COALESCE(is_active, true) AS is_active,
			          COALESCE(created_at, NOW()) AS created_at,
			          COALESCE(created_at, NOW()) AS updated_at
		`
		var m InstitutionMenu
		if err := s.pool.QueryRow(ctx, query, req.Code, req.Name, req.Path, req.Icon, req.ParentID, req.SortOrder).Scan(
			&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to create legacy menu: %w", err)
		}
		return &m, nil
	}

	query := `
		INSERT INTO institution_menus (tenant_id, name, code, path, icon, parent_id, sort_order, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, true, NOW(), NOW())
		RETURNING id, tenant_id, name, code, path, icon, parent_id, sort_order, is_active, created_at, updated_at
	`

	var m InstitutionMenu
	err = s.pool.QueryRow(ctx, query, tenantID, req.Name, req.Code, req.Path, req.Icon, req.ParentID, req.SortOrder).Scan(
		&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
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
		updates := make([]string, 0, 6)
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
		if len(updates) == 0 {
			return s.GetInstitutionMenuByID(ctx, tenantID, id)
		}

		query += fmt.Sprintf("%s WHERE id = $%d RETURNING id, NULL::bigint AS tenant_id, name, code, path, icon, parent_id, COALESCE(order_index, 0), COALESCE(is_active, true), COALESCE(created_at, NOW()), COALESCE(created_at, NOW())", strings.Join(updates, ","), argPos)
		args = append(args, id)

		var m InstitutionMenu
		if err := s.pool.QueryRow(ctx, query, args...).Scan(
			&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
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

	query += fmt.Sprintf(" WHERE id = $%d AND deleted_at IS NULL", argPos)
	args = append(args, id)

	if tenantID != nil {
		query += fmt.Sprintf(" AND (tenant_id = $%d OR tenant_id IS NULL)", argPos+1)
		args = append(args, *tenantID)
	} else {
		query += " AND tenant_id IS NULL"
	}

	query += " RETURNING id, tenant_id, name, code, path, icon, parent_id, sort_order, is_active, created_at, updated_at"

	var m InstitutionMenu
	err = s.pool.QueryRow(ctx, query, args...).Scan(
		&m.ID, &m.TenantID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
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

	// Use inst_role_menus as the canonical institution role authorization store.
	_, err = tx.Exec(ctx, "DELETE FROM inst_role_menus WHERE tenant_id = $1 AND role_code = $2", tenantID, roleCode)
	if err != nil {
		return fmt.Errorf("failed to delete existing role menus: %w", err)
	}

	for _, menuID := range permissionIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO inst_role_menus (tenant_id, role_code, menu_id, created_at)
			VALUES ($1, $2, $3, NOW())
		`, tenantID, roleCode, menuID)
		if err != nil {
			return fmt.Errorf("failed to assign role menu: %w", err)
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

	query := `
		DELETE FROM inst_role_menus
		WHERE tenant_id = $1 AND role_code = $2 AND menu_id = ANY($3)
	`

	_, err = s.pool.Exec(ctx, query, tenantID, roleCode, permissionIDs)
	if err != nil {
		return fmt.Errorf("failed to remove role menus: %w", err)
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

	query := `
		SELECT m.id,
		       m.name,
		       m.code,
		       'menu'::varchar AS resource,
		       'view'::varchar AS action,
		       m.path::varchar AS description,
		       COALESCE(m.created_at, NOW()) AS created_at,
		       COALESCE(m.created_at, NOW()) AS updated_at
		FROM inst_menus m
		JOIN inst_role_menus rm ON rm.menu_id = m.id
		WHERE rm.tenant_id = $1 AND rm.role_code = $2
		ORDER BY COALESCE(m.order_index, 0), m.id
	`

	rows, err := s.pool.Query(ctx, query, tenantID, roleCode)
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
	var tenantID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT tenant_id FROM employees WHERE id = $1 AND deleted_at IS NULL
	`, employeeID).Scan(&tenantID); err != nil {
		return nil, fmt.Errorf("employee not found: %w", err)
	}

	hasNewRoleTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return nil, err
	}
	hasNewEmployeeRoleTable, err := s.hasInstitutionEmployeeRolesTable(ctx)
	if err != nil {
		return nil, err
	}

	if hasNewRoleTable && hasNewEmployeeRoleTable {
		query := `
			SELECT r.id, r.tenant_id, r.name, r.code, r.description, r.is_active, r.created_at, r.updated_at
			FROM institution_roles r
			JOIN institution_employee_roles er ON r.id = er.role_id
			WHERE er.employee_id = $1 AND r.deleted_at IS NULL
			LIMIT 1
		`

		var r InstitutionRole
		err := s.pool.QueryRow(ctx, query, employeeID).Scan(
			&r.ID, &r.TenantID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("employee role not found: %w", err)
		}
		return &r, nil
	}

	query := `
		SELECT er.role_code,
		       COALESCE(NULLIF(r.name_cn, ''), er.role_code) AS role_name
		FROM inst_employee_roles er
		LEFT JOIN inst_roles r ON r.code = er.role_code
		WHERE er.tenant_id = $1 AND er.employee_id = $2
		ORDER BY er.created_at DESC
		LIMIT 1
	`
	var roleCode, roleName string
	if err := s.pool.QueryRow(ctx, query, tenantID, employeeID).Scan(&roleCode, &roleName); err != nil {
		return nil, fmt.Errorf("employee role not found: %w", err)
	}

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

// SetEmployeeRole sets the role for an employee
func (s *Store) SetEmployeeRole(ctx context.Context, employeeID, roleID int64) error {
	hasNewRoleTable, err := s.hasInstitutionRolesTable(ctx)
	if err != nil {
		return err
	}
	hasNewEmployeeRoleTable, err := s.hasInstitutionEmployeeRolesTable(ctx)
	if err != nil {
		return err
	}

	var tenantID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT tenant_id FROM employees WHERE id = $1 AND deleted_at IS NULL
	`, employeeID).Scan(&tenantID); err != nil {
		return fmt.Errorf("employee not found: %w", err)
	}

	if hasNewRoleTable && hasNewEmployeeRoleTable {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback(ctx)

		_, err = tx.Exec(ctx, "DELETE FROM institution_employee_roles WHERE employee_id = $1", employeeID)
		if err != nil {
			return fmt.Errorf("failed to delete existing role: %w", err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO institution_employee_roles (employee_id, role_id, created_at)
			VALUES ($1, $2, NOW())
		`, employeeID, roleID)
		if err != nil {
			return fmt.Errorf("failed to set employee role: %w", err)
		}
		return tx.Commit(ctx)
	}

	var roleCode string
	// Compatibility path:
	// - institution_roles exists (new role IDs)
	// - institution_employee_roles does not exist (still writing legacy relation table)
	// In this mixed mode we must resolve role code from institution_roles by numeric ID,
	// not by legacy hashed ID mapping.
	if hasNewRoleTable {
		if err := s.pool.QueryRow(ctx, `
			SELECT code
			FROM institution_roles
			WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		`, roleID, tenantID).Scan(&roleCode); err != nil {
			return fmt.Errorf("role not found: %w", err)
		}
	} else {
		var err error
		roleCode, err = s.findLegacyRoleCodeByID(ctx, roleID)
		if err != nil {
			return fmt.Errorf("role not found: %w", err)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		DELETE FROM inst_employee_roles
		WHERE tenant_id = $1 AND employee_id = $2
	`, tenantID, employeeID)
	if err != nil {
		return fmt.Errorf("failed to delete existing role: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO inst_employee_roles (tenant_id, employee_id, role_code, source, created_at)
		VALUES ($1, $2, $3, 'manual', NOW())
	`, tenantID, employeeID, roleCode)
	if err != nil {
		return fmt.Errorf("failed to set employee role: %w", err)
	}

	return tx.Commit(ctx)
}

// RemoveEmployeeRole removes the role from an employee
func (s *Store) RemoveEmployeeRole(ctx context.Context, employeeID int64) error {
	hasNewEmployeeRoleTable, err := s.hasInstitutionEmployeeRolesTable(ctx)
	if err != nil {
		return err
	}

	if hasNewEmployeeRoleTable {
		_, err := s.pool.Exec(ctx, `
			DELETE FROM institution_employee_roles
			WHERE employee_id = $1
		`, employeeID)
		if err != nil {
			return fmt.Errorf("failed to remove employee role: %w", err)
		}
		return nil
	}

	var tenantID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT tenant_id FROM employees WHERE id = $1 AND deleted_at IS NULL
	`, employeeID).Scan(&tenantID); err != nil {
		return fmt.Errorf("employee not found: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		DELETE FROM inst_employee_roles
		WHERE tenant_id = $1 AND employee_id = $2
	`, tenantID, employeeID)
	if err != nil {
		return fmt.Errorf("failed to remove employee role: %w", err)
	}

	return nil
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
	if hasNewRoleTable && hasNewDepartmentRoleTable {
		var r InstitutionRole
		if err := s.pool.QueryRow(ctx, `
			SELECT ir.id, ir.tenant_id, ir.name, ir.code, ir.description, ir.is_active, ir.created_at, ir.updated_at
			FROM institution_department_roles dr
			JOIN institution_roles ir ON ir.id = dr.role_id
			WHERE dr.department_id = $1
			  AND ir.deleted_at IS NULL
			ORDER BY dr.created_at DESC
			LIMIT 1
		`, departmentID).Scan(&r.ID, &r.TenantID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("department role not found: %w", err)
		}
		return &r, nil
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
	hasNewEmployeeRoleTable, err := s.hasInstitutionEmployeeRolesTable(ctx)
	if err != nil {
		return err
	}
	hasNewDepartmentRoleTable, err := s.hasInstitutionDepartmentRolesTable(ctx)
	if err != nil {
		return err
	}

	if hasNewRoleTable && hasNewDepartmentRoleTable {
		var roleID int64
		if req.RoleID != nil && *req.RoleID > 0 {
			if err := s.pool.QueryRow(ctx, `
				SELECT id
				FROM institution_roles
				WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
			`, *req.RoleID, tenantID).Scan(&roleID); err != nil {
				return fmt.Errorf("role not found: %w", err)
			}
		} else if req.RoleCode != nil && strings.TrimSpace(*req.RoleCode) != "" {
			if err := s.pool.QueryRow(ctx, `
				SELECT id
				FROM institution_roles
				WHERE code = $1 AND tenant_id = $2 AND deleted_at IS NULL
			`, strings.TrimSpace(*req.RoleCode), tenantID).Scan(&roleID); err != nil {
				return fmt.Errorf("role not found: %w", err)
			}
		} else {
			return fmt.Errorf("role_id or role_code is required")
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
			INSERT INTO institution_department_roles (department_id, role_id, is_default, created_at)
			VALUES ($1, $2, true, NOW())
		`, departmentID, roleID); err != nil {
			return fmt.Errorf("failed to set department role: %w", err)
		}

		if req.UpdateExisting {
			if hasNewEmployeeRoleTable {
				if _, err := tx.Exec(ctx, `
					DELETE FROM institution_employee_roles
					WHERE employee_id IN (
						SELECT id FROM employees WHERE tenant_id = $1 AND department_id = $2 AND deleted_at IS NULL
					)
				`, tenantID, departmentID); err != nil {
					return fmt.Errorf("failed to clear department employee roles: %w", err)
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO institution_employee_roles (employee_id, role_id, created_at)
					SELECT id, $3, NOW()
					FROM employees
					WHERE tenant_id = $1 AND department_id = $2 AND deleted_at IS NULL
				`, tenantID, departmentID, roleID); err != nil {
					return fmt.Errorf("failed to apply role to department employees: %w", err)
				}
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
	if roleCode == "" {
		return fmt.Errorf("role_code is required")
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
		if _, err := tx.Exec(ctx, `
			DELETE FROM inst_employee_roles
			WHERE tenant_id = $1
			  AND employee_id IN (SELECT id FROM employees WHERE tenant_id = $1 AND department_id = $2 AND deleted_at IS NULL)
			  AND source = 'department'
		`, tenantID, departmentID); err != nil {
			return fmt.Errorf("failed to clear department-sourced employee roles: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO inst_employee_roles (tenant_id, employee_id, role_code, source, created_at)
			SELECT $1, e.id, $3, 'department', NOW()
			FROM employees e
			WHERE e.tenant_id = $1 AND e.department_id = $2 AND e.deleted_at IS NULL
			ON CONFLICT (tenant_id, employee_id, role_code) DO NOTHING
		`, tenantID, departmentID, roleCode); err != nil {
			return fmt.Errorf("failed to apply role to department employees: %w", err)
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
