package rbac

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Operations Role operations

// ListOperationsRoles retrieves a paginated list of operations roles
func (s *Store) ListOperationsRoles(ctx context.Context, req RoleListRequest) ([]*OperationsRole, int, error) {
	query := `
		SELECT id, name, code, description, is_active, created_at, updated_at
		FROM operations_roles
		WHERE deleted_at IS NULL
	`
	args := []interface{}{}
	argPos := 1

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
	err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
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

	var roles []*OperationsRole
	for rows.Next() {
		var r OperationsRole
		err := rows.Scan(&r.ID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan role: %w", err)
		}
		roles = append(roles, &r)
	}

	return roles, total, nil
}

// GetOperationsRoleByID retrieves an operations role by ID
func (s *Store) GetOperationsRoleByID(ctx context.Context, id int64) (*OperationsRole, error) {
	query := `
		SELECT id, name, code, description, is_active, created_at, updated_at
		FROM operations_roles
		WHERE id = $1 AND deleted_at IS NULL
	`

	var r OperationsRole
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&r.ID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("role not found: %w", err)
	}

	return &r, nil
}

// GetOperationsRoleByCode retrieves an operations role by code.
func (s *Store) GetOperationsRoleByCode(ctx context.Context, code string) (*OperationsRole, error) {
	query := `
		SELECT id, name, code, description, is_active, created_at, updated_at
		FROM operations_roles
		WHERE code = $1 AND deleted_at IS NULL
	`

	var r OperationsRole
	err := s.pool.QueryRow(ctx, query, code).Scan(
		&r.ID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("role not found: %w", err)
	}

	return &r, nil
}

// CreateOperationsRole creates a new operations role
func (s *Store) CreateOperationsRole(ctx context.Context, req CreateRoleRequest) (*OperationsRole, error) {
	query := `
		INSERT INTO operations_roles (name, code, description, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, true, NOW(), NOW())
		RETURNING id, name, code, description, is_active, created_at, updated_at
	`

	var r OperationsRole
	err := s.pool.QueryRow(ctx, query, req.Name, req.Code, req.Description).Scan(
		&r.ID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create role: %w", err)
	}

	return &r, nil
}

// UpdateOperationsRole updates an operations role
func (s *Store) UpdateOperationsRole(ctx context.Context, id int64, req UpdateRoleRequest) (*OperationsRole, error) {
	query := "UPDATE operations_roles SET updated_at = NOW()"
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

	query += fmt.Sprintf(" WHERE id = $%d AND deleted_at IS NULL", argPos)
	args = append(args, id)
	query += " RETURNING id, name, code, description, is_active, created_at, updated_at"

	var r OperationsRole
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&r.ID, &r.Name, &r.Code, &r.Description, &r.IsActive, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update role: %w", err)
	}

	return &r, nil
}

// DeleteOperationsRole deletes an operations role (soft delete)
func (s *Store) DeleteOperationsRole(ctx context.Context, id int64) error {
	query := `
		UPDATE operations_roles
		SET deleted_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete role: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("role not found")
	}

	return nil
}

// AssignPermissionsToRole assigns permissions to a role
func (s *Store) AssignPermissionsToRole(ctx context.Context, roleID int64, permissionIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Delete existing permissions
	_, err = tx.Exec(ctx, "DELETE FROM operations_role_permissions WHERE role_id = $1", roleID)
	if err != nil {
		return fmt.Errorf("failed to delete existing permissions: %w", err)
	}

	// Insert new permissions
	for _, permID := range permissionIDs {
		query := `
			INSERT INTO operations_role_permissions (role_id, permission_id, created_at)
			VALUES ($1, $2, NOW())
		`
		_, err = tx.Exec(ctx, query, roleID, permID)
		if err != nil {
			return fmt.Errorf("failed to assign permission: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// RemovePermissionsFromRole removes permissions from a role
func (s *Store) RemovePermissionsFromRole(ctx context.Context, roleID int64, permissionIDs []int64) error {
	query := `
		DELETE FROM operations_role_permissions
		WHERE role_id = $1 AND permission_id = ANY($2)
	`

	_, err := s.pool.Exec(ctx, query, roleID, permissionIDs)
	if err != nil {
		return fmt.Errorf("failed to remove permissions: %w", err)
	}

	return nil
}

// GetRolePermissions retrieves permissions for a role
func (s *Store) GetRolePermissions(ctx context.Context, roleID int64) ([]*OperationsPermission, error) {
	query := `
		SELECT p.id, p.name, p.code, p.resource, p.action, p.description, p.created_at, p.updated_at
		FROM operations_permissions p
		JOIN operations_role_permissions rp ON p.id = rp.permission_id
		WHERE rp.role_id = $1
		ORDER BY p.resource, p.action
	`

	rows, err := s.pool.Query(ctx, query, roleID)
	if err != nil {
		return nil, fmt.Errorf("failed to query permissions: %w", err)
	}
	defer rows.Close()

	var permissions []*OperationsPermission
	for rows.Next() {
		var p OperationsPermission
		err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.Resource, &p.Action, &p.Description, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan permission: %w", err)
		}
		permissions = append(permissions, &p)
	}

	return permissions, nil
}

// Operations Menu operations

// ListOperationsMenus retrieves all operations menus
func (s *Store) ListOperationsMenus(ctx context.Context, req MenuListRequest) ([]*OperationsMenu, error) {
	query := `
		SELECT id, name, code, path, icon, parent_id, sort_order, is_active, created_at, updated_at
		FROM operations_menus
		WHERE deleted_at IS NULL
	`
	args := []interface{}{}
	argPos := 1

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

	var menus []*OperationsMenu
	for rows.Next() {
		var m OperationsMenu
		err := rows.Scan(&m.ID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan menu: %w", err)
		}
		menus = append(menus, &m)
	}

	return menus, nil
}

// GetOperationsMenuByID retrieves an operations menu by ID
func (s *Store) GetOperationsMenuByID(ctx context.Context, id int64) (*OperationsMenu, error) {
	query := `
		SELECT id, name, code, path, icon, parent_id, sort_order, is_active, created_at, updated_at
		FROM operations_menus
		WHERE id = $1 AND deleted_at IS NULL
	`

	var m OperationsMenu
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&m.ID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("menu not found: %w", err)
	}

	return &m, nil
}

// CreateOperationsMenu creates a new operations menu
func (s *Store) CreateOperationsMenu(ctx context.Context, req CreateMenuRequest) (*OperationsMenu, error) {
	query := `
		INSERT INTO operations_menus (name, code, path, icon, parent_id, sort_order, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, true, NOW(), NOW())
		RETURNING id, name, code, path, icon, parent_id, sort_order, is_active, created_at, updated_at
	`

	var m OperationsMenu
	err := s.pool.QueryRow(ctx, query, req.Name, req.Code, req.Path, req.Icon, req.ParentID, req.SortOrder).Scan(
		&m.ID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create menu: %w", err)
	}

	return &m, nil
}

// UpdateOperationsMenu updates an operations menu
func (s *Store) UpdateOperationsMenu(ctx context.Context, id int64, req UpdateMenuRequest) (*OperationsMenu, error) {
	query := "UPDATE operations_menus SET updated_at = NOW()"
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
	query += " RETURNING id, name, code, path, icon, parent_id, sort_order, is_active, created_at, updated_at"

	var m OperationsMenu
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&m.ID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update menu: %w", err)
	}

	return &m, nil
}

// DeleteOperationsMenu deletes an operations menu (soft delete)
func (s *Store) DeleteOperationsMenu(ctx context.Context, id int64) error {
	query := `
		UPDATE operations_menus
		SET deleted_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete menu: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("menu not found")
	}

	return nil
}

// UpdateMenuSort updates menu sort orders
func (s *Store) UpdateMenuSort(ctx context.Context, items []MenuSortItem) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, item := range items {
		query := `
			UPDATE operations_menus
			SET sort_order = $1, updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL
		`
		_, err = tx.Exec(ctx, query, item.SortOrder, item.ID)
		if err != nil {
			return fmt.Errorf("failed to update menu sort: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// GetRoleMenus retrieves menus for a role
func (s *Store) GetRoleMenus(ctx context.Context, roleID int64) ([]*OperationsMenu, error) {
	query := `
		SELECT m.id, m.name, m.code, m.path, m.icon, m.parent_id, m.sort_order, m.is_active, m.created_at, m.updated_at
		FROM operations_menus m
		JOIN operations_role_menus rm ON m.id = rm.menu_id
		WHERE rm.role_id = $1 AND m.deleted_at IS NULL
		ORDER BY m.sort_order, m.created_at
	`

	rows, err := s.pool.Query(ctx, query, roleID)
	if err != nil {
		return nil, fmt.Errorf("failed to query role menus: %w", err)
	}
	defer rows.Close()

	var menus []*OperationsMenu
	for rows.Next() {
		var m OperationsMenu
		err := rows.Scan(&m.ID, &m.Name, &m.Code, &m.Path, &m.Icon, &m.ParentID, &m.SortOrder, &m.IsActive, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan menu: %w", err)
		}
		menus = append(menus, &m)
	}

	return menus, nil
}

// AssignMenusToRole assigns menus to a role
func (s *Store) AssignMenusToRole(ctx context.Context, roleID int64, menuIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Delete existing menus
	_, err = tx.Exec(ctx, "DELETE FROM operations_role_menus WHERE role_id = $1", roleID)
	if err != nil {
		return fmt.Errorf("failed to delete existing menus: %w", err)
	}

	// Insert new menus
	for _, menuID := range menuIDs {
		query := `
			INSERT INTO operations_role_menus (role_id, menu_id, created_at)
			VALUES ($1, $2, NOW())
		`
		_, err = tx.Exec(ctx, query, roleID, menuID)
		if err != nil {
			return fmt.Errorf("failed to assign menu: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// Operations Admin operations

// ListOperationsAdmins retrieves a paginated list of operations admins
func (s *Store) ListOperationsAdmins(ctx context.Context, req AdminListRequest) ([]*AdminResponse, int, error) {
	query := `
		SELECT a.id, COALESCE(a.username, ''), COALESCE(a.email, ''),
		       CASE
		           WHEN a.is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       a.created_at, a.updated_at
		FROM operations_admins a
		WHERE a.deleted_at IS NULL
	`
	args := []interface{}{}
	argPos := 1

	if req.Username != "" {
		query += fmt.Sprintf(" AND a.username ILIKE $%d", argPos)
		args = append(args, "%"+req.Username+"%")
		argPos++
	}

	if req.Email != "" {
		query += fmt.Sprintf(" AND a.email ILIKE $%d", argPos)
		args = append(args, "%"+req.Email+"%")
		argPos++
	}

	if req.IsActive != nil {
		if *req.IsActive {
			query += " AND a.is_active::text IN ('1','t','true','TRUE')"
		} else {
			query += " AND a.is_active::text NOT IN ('1','t','true','TRUE')"
		}
	}

	// Count total
	countQuery := "SELECT COUNT(*) FROM (" + query + ") AS count_query"
	var total int
	err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count admins: %w", err)
	}

	// Add pagination
	query += " ORDER BY a.created_at DESC"
	offset := (req.Page - 1) * req.PageSize
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query admins: %w", err)
	}
	defer rows.Close()

	var admins []*AdminResponse
	for rows.Next() {
		var admin AdminResponse
		err := rows.Scan(&admin.ID, &admin.Username, &admin.Email, &admin.IsActive, &admin.CreatedAt, &admin.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan admin: %w", err)
		}

		// Get roles for this admin
		roles, err := s.GetAdminRoles(ctx, admin.ID)
		if err == nil {
			admin.Roles = roles
		}

		admins = append(admins, &admin)
	}

	return admins, total, nil
}

// GetAdminRoles retrieves roles for an admin
func (s *Store) GetAdminRoles(ctx context.Context, adminID int64) ([]RoleResponse, error) {
	query := `
		SELECT r.id, r.name, r.code, r.description, r.is_active, r.created_at, r.updated_at
		FROM operations_roles r
		JOIN operations_admin_roles ar ON r.id = ar.role_id
		WHERE ar.admin_id = $1 AND r.deleted_at IS NULL
		ORDER BY r.name
	`

	rows, err := s.pool.Query(ctx, query, adminID)
	if err != nil {
		return nil, fmt.Errorf("failed to query admin roles: %w", err)
	}
	defer rows.Close()

	var roles []RoleResponse
	for rows.Next() {
		var role RoleResponse
		err := rows.Scan(&role.ID, &role.Name, &role.Code, &role.Description, &role.IsActive, &role.CreatedAt, &role.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan role: %w", err)
		}
		roles = append(roles, role)
	}

	return roles, nil
}

// GetOperationsAdminByID retrieves an operations admin by ID
func (s *Store) GetOperationsAdminByID(ctx context.Context, id int64) (*AdminResponse, error) {
	query := `
		SELECT id, COALESCE(username, ''), COALESCE(email, ''),
		       CASE
		           WHEN is_active::text IN ('1','t','true','TRUE') THEN true
		           ELSE false
		       END AS is_active,
		       created_at, updated_at
		FROM operations_admins
		WHERE id = $1 AND deleted_at IS NULL
	`

	var admin AdminResponse
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&admin.ID, &admin.Username, &admin.Email, &admin.IsActive, &admin.CreatedAt, &admin.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("admin not found: %w", err)
	}

	// Get roles
	roles, err := s.GetAdminRoles(ctx, id)
	if err == nil {
		admin.Roles = roles
	}

	return &admin, nil
}

// CreateOperationsAdmin creates a new operations admin
func (s *Store) CreateOperationsAdmin(ctx context.Context, req CreateAdminRequest, hashedPassword string) (*AdminResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Create admin
	query := `
		INSERT INTO operations_admins (username, password_hash, email, is_active, session_version, created_at, updated_at)
		VALUES ($1, $2, $3, true, 1, NOW(), NOW())
		RETURNING id, username, email, is_active, created_at, updated_at
	`

	var admin AdminResponse
	err = tx.QueryRow(ctx, query, req.Username, hashedPassword, req.Email).Scan(
		&admin.ID, &admin.Username, &admin.Email, &admin.IsActive, &admin.CreatedAt, &admin.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin: %w", err)
	}

	// Assign roles
	if len(req.RoleIDs) > 0 {
		for _, roleID := range req.RoleIDs {
			roleQuery := `
				INSERT INTO operations_admin_roles (admin_id, role_id, created_at)
				VALUES ($1, $2, NOW())
			`
			_, err = tx.Exec(ctx, roleQuery, admin.ID, roleID)
			if err != nil {
				return nil, fmt.Errorf("failed to assign role: %w", err)
			}
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Get roles
	roles, _ := s.GetAdminRoles(ctx, admin.ID)
	admin.Roles = roles

	return &admin, nil
}

// UpdateOperationsAdmin updates an operations admin
func (s *Store) UpdateOperationsAdmin(ctx context.Context, id int64, req UpdateAdminRequest) (*AdminResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := "UPDATE operations_admins SET updated_at = NOW()"
	args := []interface{}{}
	argPos := 1

	if req.Email != nil {
		query += fmt.Sprintf(", email = $%d", argPos)
		args = append(args, *req.Email)
		argPos++
	}

	if req.IsActive != nil {
		query += fmt.Sprintf(", is_active = $%d", argPos)
		args = append(args, *req.IsActive)
		argPos++
	}

	query += fmt.Sprintf(" WHERE id = $%d AND deleted_at IS NULL", argPos)
	args = append(args, id)
	query += " RETURNING id, username, email, is_active, created_at, updated_at"

	var admin AdminResponse
	err = tx.QueryRow(ctx, query, args...).Scan(
		&admin.ID, &admin.Username, &admin.Email, &admin.IsActive, &admin.CreatedAt, &admin.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update admin: %w", err)
	}

	// Update roles if provided
	if req.RoleIDs != nil {
		// Delete existing roles
		_, err = tx.Exec(ctx, "DELETE FROM operations_admin_roles WHERE admin_id = $1", id)
		if err != nil {
			return nil, fmt.Errorf("failed to delete existing roles: %w", err)
		}

		// Insert new roles
		for _, roleID := range req.RoleIDs {
			roleQuery := `
				INSERT INTO operations_admin_roles (admin_id, role_id, created_at)
				VALUES ($1, $2, NOW())
			`
			_, err = tx.Exec(ctx, roleQuery, id, roleID)
			if err != nil {
				return nil, fmt.Errorf("failed to assign role: %w", err)
			}
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Get roles
	roles, _ := s.GetAdminRoles(ctx, id)
	admin.Roles = roles

	return &admin, nil
}

// DeleteOperationsAdmin deletes an operations admin (soft delete)
func (s *Store) DeleteOperationsAdmin(ctx context.Context, id int64) error {
	query := `
		UPDATE operations_admins
		SET deleted_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete admin: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("admin not found")
	}

	return nil
}

// ResetAdminPassword resets an admin's password
func (s *Store) ResetAdminPassword(ctx context.Context, id int64, hashedPassword string) error {
	query := `
		UPDATE operations_admins
		SET password_hash = $1, session_version = session_version + 1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, hashedPassword, id)
	if err != nil {
		return fmt.Errorf("failed to reset password: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("admin not found")
	}

	return nil
}
