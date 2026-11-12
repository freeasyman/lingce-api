package dealexpertpermission

import "time"

const (
	RoleAdmin = "deal_admin"
	RoleStaff = "deal_staff"

	AbilityConsultationRead = "deal.consultation.read"
	AbilityTaskRead         = "deal.task.read"
	AbilityTaskExecute      = "deal.task.execute"
	AbilityPerformanceRead  = "deal.performance.read"
	AbilityProductRead      = "deal.product.read"
	AbilityWorkbenchRead    = "deal.workbench.read"
	AbilityLedgerRead       = "deal.ledger.read"
	AbilityLedgerExport     = "deal.ledger.export"
	AbilityLedgerCorrect    = "deal.ledger.correct"
	AbilityQuotesRead       = "deal.quotes.read"
	AbilityQuotesManage     = "deal.quotes.manage"
	AbilityProductManage    = "deal.product.manage"
	AbilityPermissionManage = "deal.permission.manage"
)

// AllAbilities 是能力码的固定顺序（权限页展示顺序与此一致）。
var AllAbilities = []string{
	AbilityConsultationRead,
	AbilityTaskRead,
	AbilityTaskExecute,
	AbilityPerformanceRead,
	AbilityProductRead,
	AbilityWorkbenchRead,
	AbilityLedgerRead,
	AbilityLedgerExport,
	AbilityLedgerCorrect,
	AbilityQuotesRead,
	AbilityQuotesManage,
	AbilityProductManage,
	AbilityPermissionManage,
}

// DefaultStaffAbilities 是咨询师（deal_staff）默认能力包。
var DefaultStaffAbilities = []string{
	AbilityConsultationRead,
	AbilityTaskRead,
	AbilityTaskExecute,
	AbilityPerformanceRead,
	AbilityProductRead,
}

// dealManagerRoles 是“负责人”类角色：未配置权限时默认全开（deal_admin）。
var dealManagerRoles = map[string]bool{
	"admin":                true,
	"director":             true,
	"president":            true,
	"dean":                 true,
	"manager":              true,
	"sales_manager":        true,
	"operating_manager":    true,
	"reception_manager":    true,
	"operating_supervisor": true,
	"marketing_manager":    true,
}

func IsManagerRole(role string) bool {
	return dealManagerRoles[role]
}

type Assignment struct {
	ID                 int64     `json:"id"`
	TenantID           int64     `json:"tenant_id"`
	EmployeeID         int64     `json:"employee_id"`
	Enabled            bool      `json:"enabled"`
	RoleCode           string    `json:"role_code"`
	Abilities          []string  `json:"abilities"`
	DataScope          string    `json:"data_scope"`
	VisibleEmployeeIDs []int64   `json:"visible_employee_ids"`
	CreatedBy          *int64    `json:"created_by,omitempty"`
	UpdatedBy          *int64    `json:"updated_by,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// DefaultAdminAssignment 返回机构管理员的生效权限视图。
// 它不是数据库行（ID=0），用于管理员尚未写入 deal_expert_permission_assignments
// 时让权限接口返回管理员权限，而不是 null。
func DefaultAdminAssignment(tenantID, employeeID int64) *Assignment {
	now := time.Now()
	return &Assignment{
		ID:                 0,
		TenantID:           tenantID,
		EmployeeID:         employeeID,
		Enabled:            true,
		RoleCode:           RoleAdmin,
		Abilities:          append([]string{}, AllAbilities...),
		DataScope:          DataScopeTenant,
		VisibleEmployeeIDs: []int64{},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

type EmployeePermissionRow struct {
	EmployeeID   int64       `json:"employee_id"`
	Username     string      `json:"username"`
	FullName     string      `json:"full_name"`
	Phone        string      `json:"phone"`
	DepartmentID int64       `json:"department_id,omitempty"`
	Department   string      `json:"department_name,omitempty"`
	IsActive     bool        `json:"is_active"`
	Role         string      `json:"role"`
	RoleCodes    []string    `json:"role_codes,omitempty"`
	Permission   *Assignment `json:"permission,omitempty"`
}

type Access struct {
	UserID             int64
	TenantID           int64
	Admin              bool
	Enabled            bool
	RoleCode           string
	Abilities          map[string]struct{}
	DataScope          string
	VisibleEmployeeIDs []int64
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
	return a != nil && (a.Admin || a.Has(AbilityPermissionManage))
}

func (a *Access) AbilityList() []string {
	if a == nil {
		return []string{}
	}
	result := make([]string, 0, len(AllAbilities))
	for _, ability := range AllAbilities {
		if a.Admin {
			result = append(result, ability)
			continue
		}
		if _, ok := a.Abilities[ability]; ok {
			result = append(result, ability)
		}
	}
	return result
}

// Visibility 返回当前生效的数据可见集合；管理员始终为全机构。
func (a *Access) Visibility() *Visibility {
	if a == nil {
		return &Visibility{Scope: DataScopeSelf, EmployeeIDs: []int64{}}
	}
	if a.Admin {
		return &Visibility{Scope: DataScopeTenant, All: true, EmployeeIDs: []int64{}}
	}
	scope := NormalizeDataScope(a.DataScope)
	return &Visibility{Scope: scope, EmployeeIDs: NormalizeVisibleEmployeeIDs(a.VisibleEmployeeIDs)}
}
