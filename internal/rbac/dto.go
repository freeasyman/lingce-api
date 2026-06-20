package rbac

import "time"

// Operations RBAC DTOs

// RoleListRequest represents a request to list roles
type RoleListRequest struct {
	Name     string `json:"name,omitempty"`
	Code     string `json:"code,omitempty"`
	IsActive *bool  `json:"is_active,omitempty"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// CreateRoleRequest represents a request to create a role
type CreateRoleRequest struct {
	Name        string  `json:"name"`
	Code        string  `json:"code"`
	Description *string `json:"description,omitempty"`
}

// UpdateRoleRequest represents a request to update a role
type UpdateRoleRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	IsActive    *bool   `json:"is_active,omitempty"`
}

// RoleResponse represents a role response
type RoleResponse struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Description *string   `json:"description,omitempty"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AssignPermissionsRequest represents a request to assign permissions to a role
type AssignPermissionsRequest struct {
	PermissionIDs []int64 `json:"permission_ids"`
}

// PermissionResponse represents a permission response
type PermissionResponse struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Code        string  `json:"code"`
	Resource    string  `json:"resource"`
	Action      string  `json:"action"`
	Description *string `json:"description,omitempty"`
}

// MenuListRequest represents a request to list menus
type MenuListRequest struct {
	Name     string `json:"name,omitempty"`
	IsActive *bool  `json:"is_active,omitempty"`
}

// CreateMenuRequest represents a request to create a menu
type CreateMenuRequest struct {
	Name      string  `json:"name"`
	Code      string  `json:"code"`
	Path      *string `json:"path,omitempty"`
	Icon      *string `json:"icon,omitempty"`
	ParentID  *int64  `json:"parent_id,omitempty"`
	SortOrder int     `json:"sort_order"`
}

// UpdateMenuRequest represents a request to update a menu
type UpdateMenuRequest struct {
	Name      *string `json:"name,omitempty"`
	Path      *string `json:"path,omitempty"`
	Icon      *string `json:"icon,omitempty"`
	ParentID  *int64  `json:"parent_id,omitempty"`
	SortOrder *int    `json:"sort_order,omitempty"`
	IsActive  *bool   `json:"is_active,omitempty"`
}

// MenuResponse represents a menu response
type MenuResponse struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Code      string          `json:"code"`
	Path      *string         `json:"path,omitempty"`
	Icon      *string         `json:"icon,omitempty"`
	ParentID  *int64          `json:"parent_id,omitempty"`
	SortOrder int             `json:"sort_order"`
	IsActive  bool            `json:"is_active"`
	Children  []*MenuResponse `json:"children,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// MenuSortRequest represents a request to sort menus
type MenuSortRequest struct {
	Items []MenuSortItem `json:"items"`
}

// MenuSortItem represents a menu sort item
type MenuSortItem struct {
	ID        int64 `json:"id"`
	SortOrder int   `json:"sort_order"`
}

// AdminListRequest represents a request to list admins
type AdminListRequest struct {
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`
	IsActive *bool  `json:"is_active,omitempty"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// CreateAdminRequest represents a request to create an admin
type CreateAdminRequest struct {
	Username string  `json:"username"`
	Password string  `json:"password"`
	Email    *string `json:"email,omitempty"`
	RoleIDs  []int64 `json:"role_ids,omitempty"`
}

// UpdateAdminRequest represents a request to update an admin
type UpdateAdminRequest struct {
	Email    *string `json:"email,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
	RoleIDs  []int64 `json:"role_ids,omitempty"`
}

// AdminResponse represents an admin response
type AdminResponse struct {
	ID        int64          `json:"id"`
	Username  string         `json:"username"`
	Email     *string        `json:"email,omitempty"`
	IsActive  bool           `json:"is_active"`
	Roles     []RoleResponse `json:"roles,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// ResetPasswordRequest represents a request to reset password
type ResetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// AssignMenusRequest represents a request to assign menus to a role
type AssignMenusRequest struct {
	MenuIDs []int64 `json:"menu_ids"`
}

// Institution RBAC DTOs

// InstitutionRoleListRequest represents a request to list institution roles
type InstitutionRoleListRequest struct {
	Name     string `json:"name,omitempty"`
	Code     string `json:"code,omitempty"`
	IsActive *bool  `json:"is_active,omitempty"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// CreateInstitutionRoleRequest represents a request to create an institution role
type CreateInstitutionRoleRequest struct {
	Name        string  `json:"name"`
	Code        string  `json:"code"`
	Description *string `json:"description,omitempty"`
}

// UpdateInstitutionRoleRequest represents a request to update an institution role
type UpdateInstitutionRoleRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	IsActive    *bool   `json:"is_active,omitempty"`
}

// InstitutionRoleResponse represents an institution role response
type InstitutionRoleResponse struct {
	ID          int64     `json:"id"`
	TenantID    int64     `json:"tenant_id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Description *string   `json:"description,omitempty"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// InstitutionMenuListRequest represents a request to list institution menus
type InstitutionMenuListRequest struct {
	Name     string `json:"name,omitempty"`
	IsActive *bool  `json:"is_active,omitempty"`
}

// CreateInstitutionMenuRequest represents a request to create an institution menu
type CreateInstitutionMenuRequest struct {
	Name                string  `json:"name"`
	Code                string  `json:"code"`
	Path                *string `json:"path,omitempty"`
	Icon                *string `json:"icon,omitempty"`
	ParentID            *int64  `json:"parent_id,omitempty"`
	SortOrder           int     `json:"sort_order"`
	IsFeatureAssignable bool    `json:"is_feature_assignable,omitempty"`
	IsDefaultForAdmin   bool    `json:"is_default_for_admin,omitempty"`
	FeatureCode         *string `json:"feature_code,omitempty"`
	FeatureName         *string `json:"feature_name,omitempty"`
}

// UpdateInstitutionMenuRequest represents a request to update an institution menu
type UpdateInstitutionMenuRequest struct {
	Name                *string `json:"name,omitempty"`
	Path                *string `json:"path,omitempty"`
	Icon                *string `json:"icon,omitempty"`
	ParentID            *int64  `json:"parent_id,omitempty"`
	SortOrder           *int    `json:"sort_order,omitempty"`
	IsActive            *bool   `json:"is_active,omitempty"`
	IsFeatureAssignable *bool   `json:"is_feature_assignable,omitempty"`
	IsDefaultForAdmin   *bool   `json:"is_default_for_admin,omitempty"`
	FeatureCode         *string `json:"feature_code,omitempty"`
	FeatureName         *string `json:"feature_name,omitempty"`
}

// InstitutionMenuResponse represents an institution menu response
type InstitutionMenuResponse struct {
	ID                  int64                      `json:"id"`
	TenantID            *int64                     `json:"tenant_id,omitempty"`
	Name                string                     `json:"name"`
	Code                string                     `json:"code"`
	Path                *string                    `json:"path,omitempty"`
	Icon                *string                    `json:"icon,omitempty"`
	ParentID            *int64                     `json:"parent_id,omitempty"`
	SortOrder           int                        `json:"sort_order"`
	IsActive            bool                       `json:"is_active"`
	IsFeatureAssignable bool                       `json:"is_feature_assignable"`
	IsDefaultForAdmin   bool                       `json:"is_default_for_admin"`
	FeatureCode         *string                    `json:"feature_code,omitempty"`
	FeatureName         *string                    `json:"feature_name,omitempty"`
	Children            []*InstitutionMenuResponse `json:"children,omitempty"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`
}

// EmployeeRoleResponse represents an employee role response
type EmployeeRoleResponse struct {
	EmployeeID int64                     `json:"employee_id"`
	Roles      []InstitutionRoleResponse `json:"roles"`
}

// EmployeeEffectiveMenuResponse represents effective menus for an employee.
type EmployeeEffectiveMenuResponse struct {
	EmployeeID int64                      `json:"employee_id"`
	TenantID   int64                      `json:"tenant_id"`
	RoleCode   string                     `json:"role_code,omitempty"`
	MenuCodes  []string                   `json:"menu_codes"`
	Menus      []*InstitutionMenuResponse `json:"menus"`
}

// SetEmployeeRoleRequest represents a request to set employee role
type SetEmployeeRoleRequest struct {
	RoleID int64 `json:"role_id"`
}

// DepartmentRoleResponse represents a department default role response.
type DepartmentRoleResponse struct {
	DepartmentID int64                     `json:"department_id"`
	Roles        []InstitutionRoleResponse `json:"roles"`
}

// SetDepartmentRoleRequest represents a request to set department default role.
type SetDepartmentRoleRequest struct {
	RoleID         *int64  `json:"role_id,omitempty"`
	RoleCode       *string `json:"role_code,omitempty"`
	UpdateExisting bool    `json:"update_existing"`
}
