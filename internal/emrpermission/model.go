package emrpermission

import "time"

type Access struct {
	UserID       int64
	TenantID     int64
	DepartmentID *int64
	Scope        string
	Role         string
	Admin        bool
	Abilities    map[string]struct{}
}

func (a *Access) CanManagePermissions() bool {
	return a != nil && (a.Admin || a.Role == "emr_admin")
}

func (a *Access) Has(ability string) bool {
	if a == nil {
		return false
	}
	if a.Admin {
		return true
	}
	if ability == "record.create" || ability == "record.submit" {
		if _, ok := a.Abilities[ability]; ok {
			return true
		}
		ability = "record.edit"
	}
	_, ok := a.Abilities[ability]
	return ok
}

func (a *Access) CanRecord(doctorID int64, departmentID *int64) bool {
	if a == nil {
		return false
	}
	if a.Admin || a.Scope == "tenant" {
		return true
	}
	if a.Scope == "self" {
		return doctorID == a.UserID
	}
	return a.Scope == "department" && a.DepartmentID != nil && departmentID != nil && *a.DepartmentID == *departmentID
}

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
