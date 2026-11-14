package emrpermission

import "time"

// AllAbilities 是听写病历管理员默认拥有的完整能力包。
// 与 emr_permission_assignments 里管理员保存的能力码保持一致。
var AllAbilities = []string{
	"record.read",
	"record.read_all",
	"record.create",
	"record.edit",
	"record.submit",
	"record.archive",
	"record.history.read",
	"record.export",
	"template.manage",
	"quality.manage",
}

// DefaultAdminAssignment 返回机构管理员的生效权限视图。
// 它不是数据库行（ID=0），用于管理员尚未写入 emr_permission_assignments 时
// 让权限接口返回管理员权限，而不是 null。
func DefaultAdminAssignment(tenantID, employeeID int64) *Assignment {
	now := time.Now()
	return &Assignment{
		ID:          0,
		TenantID:    tenantID,
		EmployeeID:  employeeID,
		Enabled:     true,
		EmrRoleCode: "emr_admin",
		Scope:       "tenant",
		Abilities:   append([]string{}, AllAbilities...),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

type Access struct {
	UserID       int64
	TenantID      int64
	DepartmentIDs []int64
	Scope         string
	Role          string
	Admin         bool
	Abilities     map[string]struct{}
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
	if a.Scope == "department" && departmentID != nil {
		for _, id := range a.DepartmentIDs {
			if id == *departmentID {
				return true
			}
		}
	}
	return false
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
	EmployeeID            int64       `json:"employee_id"`
	Username              string      `json:"username"`
	FullName              string      `json:"full_name"`
	Phone                 string      `json:"phone"`
	DepartmentName        string      `json:"department_name,omitempty"`
	MedicalDepartmentCode string      `json:"medical_department_code,omitempty"`
	OrgRoleCode           string      `json:"org_role_code,omitempty"`
	OrgRoleName           string      `json:"org_role_name,omitempty"`
	IsActive              bool        `json:"is_active"`
	EmrPermission         *Assignment `json:"emr_permission,omitempty"`
}
