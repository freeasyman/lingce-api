package rbac

import (
	"context"
	"fmt"
	"strings"
)

// PermissionChecker provides permission checking functionality
type PermissionChecker struct {
	store *Store
}

// NewPermissionChecker creates a new permission checker
func NewPermissionChecker(store *Store) *PermissionChecker {
	return &PermissionChecker{store: store}
}

// Permission represents a parsed permission
type Permission struct {
	Resource string
	Action   string
}

// ParsePermission parses a permission string (format: "resource:action")
func ParsePermission(permStr string) (Permission, error) {
	parts := strings.Split(permStr, ":")
	if len(parts) != 2 {
		return Permission{}, fmt.Errorf("invalid permission format: %s (expected resource:action)", permStr)
	}
	return Permission{
		Resource: parts[0],
		Action:   parts[1],
	}, nil
}

// GetAdminPermissions retrieves all permissions for an admin user
// Query: admins → admin_roles → roles → role_permissions → permissions (single JOIN)
func (c *PermissionChecker) GetAdminPermissions(ctx context.Context, adminID int64) ([]Permission, error) {
	query := `
		SELECT DISTINCT p.resource, p.action
		FROM operations_permissions p
		JOIN operations_role_permissions rp ON p.id = rp.permission_id
		JOIN operations_admin_roles ar ON rp.role_id = ar.role_id
		WHERE ar.admin_id = $1
		ORDER BY p.resource, p.action
	`

	rows, err := c.store.pool.Query(ctx, query, adminID)
	if err != nil {
		return nil, fmt.Errorf("failed to query admin permissions: %w", err)
	}
	defer rows.Close()

	var permissions []Permission
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.Resource, &p.Action); err != nil {
			return nil, fmt.Errorf("failed to scan permission: %w", err)
		}
		permissions = append(permissions, p)
	}

	return permissions, nil
}

// GetEmployeePermissions retrieves all permissions for an employee user
// Query: employees → employee_roles → roles → role_permissions → permissions (single JOIN)
func (c *PermissionChecker) GetEmployeePermissions(ctx context.Context, employeeID int64) ([]Permission, error) {
	query := `
		SELECT DISTINCT p.resource, p.action
		FROM institution_permissions p
		JOIN institution_role_permissions rp ON p.id = rp.permission_id
		JOIN institution_employee_roles er ON rp.role_id = er.role_id
		WHERE er.employee_id = $1
		ORDER BY p.resource, p.action
	`

	rows, err := c.store.pool.Query(ctx, query, employeeID)
	if err != nil {
		return nil, fmt.Errorf("failed to query employee permissions: %w", err)
	}
	defer rows.Close()

	var permissions []Permission
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.Resource, &p.Action); err != nil {
			return nil, fmt.Errorf("failed to scan permission: %w", err)
		}
		permissions = append(permissions, p)
	}

	return permissions, nil
}

// HasPermission checks if a user has a specific permission
// userType: "admin", "employee", "mobile"
// permission: "resource:action" format (e.g., "recording:write")
func (c *PermissionChecker) HasPermission(ctx context.Context, userID int64, userType, permission string) (bool, error) {
	perm, err := ParsePermission(permission)
	if err != nil {
		return false, err
	}

	var query string
	switch userType {
	case "admin":
		query = `
			SELECT EXISTS (
				SELECT 1
				FROM operations_permissions p
				JOIN operations_role_permissions rp ON p.id = rp.permission_id
				JOIN operations_admin_roles ar ON rp.role_id = ar.role_id
				WHERE ar.admin_id = $1
				  AND p.resource = $2
				  AND p.action = $3
			)
		`
	case "employee":
		query = `
			SELECT EXISTS (
				SELECT 1
				FROM institution_permissions p
				JOIN institution_role_permissions rp ON p.id = rp.permission_id
				JOIN institution_employee_roles er ON rp.role_id = er.role_id
				WHERE er.employee_id = $1
				  AND p.resource = $2
				  AND p.action = $3
			)
		`
	case "mobile":
		// Mobile users typically don't have RBAC permissions
		// They rely on user type checks and tenant scope
		return false, nil
	default:
		return false, fmt.Errorf("unknown user type: %s", userType)
	}

	var hasPermission bool
	err = c.store.pool.QueryRow(ctx, query, userID, perm.Resource, perm.Action).Scan(&hasPermission)
	if err != nil {
		return false, fmt.Errorf("failed to check permission: %w", err)
	}

	return hasPermission, nil
}
