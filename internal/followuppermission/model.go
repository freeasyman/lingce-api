package followuppermission

import "time"

const (
	RoleAdmin = "followup_admin"
	RoleStaff = "followup_staff"

	AbilityTaskRead    = "followup.task.read"
	AbilityTaskExecute = "followup.task.execute"
	AbilityTaskAssign  = "followup.task.assign"
	AbilityPartnership = "followup.partnership.manage"
	AbilityScope       = "followup.scope.manage"
	AbilityPermission  = "followup.permission.manage"
)

var AllAbilities = []string{
	AbilityTaskRead,
	AbilityTaskExecute,
	AbilityTaskAssign,
	AbilityPartnership,
	AbilityScope,
	AbilityPermission,
}

type Assignment struct {
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

type EmployeePermissionRow struct {
	EmployeeID int64       `json:"employee_id"`
	Username   string      `json:"username"`
	FullName   string      `json:"full_name"`
	Phone      string      `json:"phone"`
	Department string      `json:"department_name,omitempty"`
	IsActive   bool        `json:"is_active"`
	Permission *Assignment `json:"permission,omitempty"`
}

type Access struct {
	UserID    int64
	TenantID  int64
	Admin     bool
	Enabled   bool
	RoleCode  string
	Abilities map[string]struct{}
}

func (a *Access) Has(ability string) bool {
	if a == nil || !a.Enabled {
		return false
	}
	if a.Admin {
		return true
	}
	_, ok := a.Abilities[ability]
	return ok
}

func (a *Access) CanManagePermissions() bool {
	return a != nil && (a.Admin || a.Has(AbilityPermission))
}
