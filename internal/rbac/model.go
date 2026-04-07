package rbac

import "time"

// OperationsRole represents an operations admin role
type OperationsRole struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Code        string     `json:"code"`
	Description *string    `json:"description,omitempty"`
	IsActive    bool       `json:"is_active"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// OperationsMenu represents an operations menu item
type OperationsMenu struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	Path      *string    `json:"path,omitempty"`
	Icon      *string    `json:"icon,omitempty"`
	ParentID  *int64     `json:"parent_id,omitempty"`
	SortOrder int        `json:"sort_order"`
	IsActive  bool       `json:"is_active"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// OperationsPermission represents a permission
type OperationsPermission struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Resource    string    `json:"resource"`
	Action      string    `json:"action"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// OperationsRolePermission represents role-permission assignment
type OperationsRolePermission struct {
	ID           int64     `json:"id"`
	RoleID       int64     `json:"role_id"`
	PermissionID int64     `json:"permission_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// OperationsRoleMenu represents role-menu assignment
type OperationsRoleMenu struct {
	ID        int64     `json:"id"`
	RoleID    int64     `json:"role_id"`
	MenuID    int64     `json:"menu_id"`
	CreatedAt time.Time `json:"created_at"`
}

// OperationsAdminRole represents admin-role assignment
type OperationsAdminRole struct {
	ID        int64     `json:"id"`
	AdminID   int64     `json:"admin_id"`
	RoleID    int64     `json:"role_id"`
	CreatedAt time.Time `json:"created_at"`
}

// InstitutionRole represents an institution role
type InstitutionRole struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	Name        string     `json:"name"`
	Code        string     `json:"code"`
	Description *string    `json:"description,omitempty"`
	IsActive    bool       `json:"is_active"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// InstitutionMenu represents an institution menu item
type InstitutionMenu struct {
	ID        int64      `json:"id"`
	TenantID  *int64     `json:"tenant_id,omitempty"` // NULL means global menu
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	Path      *string    `json:"path,omitempty"`
	Icon      *string    `json:"icon,omitempty"`
	ParentID  *int64     `json:"parent_id,omitempty"`
	SortOrder int        `json:"sort_order"`
	IsActive  bool       `json:"is_active"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// InstitutionPermission represents an institution permission
type InstitutionPermission struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Resource    string    `json:"resource"`
	Action      string    `json:"action"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// InstitutionRolePermission represents institution role-permission assignment
type InstitutionRolePermission struct {
	ID           int64     `json:"id"`
	RoleID       int64     `json:"role_id"`
	PermissionID int64     `json:"permission_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// InstitutionEmployeeRole represents employee-role assignment
type InstitutionEmployeeRole struct {
	ID         int64     `json:"id"`
	EmployeeID int64     `json:"employee_id"`
	RoleID     int64     `json:"role_id"`
	CreatedAt  time.Time `json:"created_at"`
}
