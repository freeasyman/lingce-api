package emrpermission

import "time"

type Assignment struct {
	ID          int64     `json:"id"`
	TenantID    int64     `json:"tenant_id"`
	EmployeeID  int64     `json:"employee_id"`
	Enabled     bool      `json:"enabled"`
	EmrRoleCode string    `json:"emr_role_code"`
	Scope       string    `json:"scope"`
	Abilities   []string  `json:"abilities"`
	CreatedBy   *int64    `json:"created_by,omitempty"`
	UpdatedBy   *int64    `json:"updated_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type EmployeePermissionRow struct {
	EmployeeID     int64       `json:"employee_id"`
	Username       string      `json:"username"`
	FullName       string      `json:"full_name"`
	Phone          string      `json:"phone"`
	DepartmentName string      `json:"department_name,omitempty"`
	OrgRoleCode    string      `json:"org_role_code,omitempty"`
	OrgRoleName    string      `json:"org_role_name,omitempty"`
	IsActive       bool        `json:"is_active"`
	EmrPermission  *Assignment `json:"emr_permission,omitempty"`
}
