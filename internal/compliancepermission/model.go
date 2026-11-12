package compliancepermission

import "time"

const (
	RoleAdmin = "compliance_admin"
	RoleStaff = "compliance_staff"

	AbilityFindingRead   = "compliance.finding.read"
	AbilityFindingReview = "compliance.finding.review"
	AbilityRuleRead      = "compliance.rule.read"
	AbilityRuleManage    = "compliance.rule.manage"
	AbilityRunRead       = "compliance.run.read"
	AbilityPermission    = "compliance.permission.manage"
)

var AllAbilities = []string{
	AbilityFindingRead,
	AbilityFindingReview,
	AbilityRuleRead,
	AbilityRuleManage,
	AbilityRunRead,
	AbilityPermission,
}

var AdminAbilities = append([]string(nil), AllAbilities...)

var StaffAbilities = []string{
	AbilityFindingRead,
	AbilityFindingReview,
	AbilityRuleRead,
	AbilityRunRead,
}

// DefaultAdminAssignment 返回机构管理员的生效权限视图。
// 它不是数据库行（ID=0），用于管理员尚未写入 compliance_permission_assignments
// 时让权限接口返回管理员权限，而不是 null。
func DefaultAdminAssignment(tenantID, employeeID int64) *Assignment {
	now := time.Now()
	return &Assignment{
		ID:         0,
		TenantID:   tenantID,
		EmployeeID: employeeID,
		Enabled:    true,
		RoleCode:   RoleAdmin,
		Abilities:  append([]string{}, AdminAbilities...),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
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

type UpsertRequest struct {
	Enabled   bool     `json:"enabled"`
	RoleCode  string   `json:"role_code"`
	Abilities []string `json:"abilities"`
}
