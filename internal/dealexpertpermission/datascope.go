package dealexpertpermission

import "strings"

// 数据可见范围（与产品方案一致，只服务赢客专家，不做跨产品抽象）。
const (
	DataScopeSelf   = "self"
	DataScopeCustom = "custom"
	DataScopeTenant = "tenant"
)

// Visibility 是一次请求解析后的员工可见集合。
// All=true 表示全机构（tenant）；否则 EmployeeIDs 是精确集合（self 只有当前员工，custom 为指定员工）。
type Visibility struct {
	Scope       string  `json:"data_scope"`
	All         bool    `json:"all"`
	EmployeeIDs []int64 `json:"visible_employee_ids"`
}

// Allows 判断某个员工归属的数据对当前请求是否可见。
func (v *Visibility) Allows(employeeID int64) bool {
	if v == nil {
		return false
	}
	if v.All {
		return true
	}
	for _, id := range v.EmployeeIDs {
		if id == employeeID {
			return true
		}
	}
	return false
}

// NormalizeDataScope 把写入值收敛到三个合法枚举；空值按 self 处理。
func NormalizeDataScope(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case DataScopeCustom:
		return DataScopeCustom
	case DataScopeTenant:
		return DataScopeTenant
	default:
		return DataScopeSelf
	}
}

// DefaultDataScopeForRole 未显式配置时的默认范围：
// 机构管理员 tenant，负责人/主管 custom，其余咨询师 self。
func DefaultDataScopeForRole(employeeRole string) string {
	role := strings.ToLower(strings.TrimSpace(employeeRole))
	if role == "admin" {
		return DataScopeTenant
	}
	if IsManagerRole(role) {
		return DataScopeCustom
	}
	return DataScopeSelf
}

// NormalizeVisibleEmployeeIDs 去重、丢弃非法 ID，并保持传入顺序。
// 明确返回非 nil 切片，保证 JSON 输出是 [] 而不是 null。
func NormalizeVisibleEmployeeIDs(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// ResolveVisibility 把「配置」解析成「生效可见集合」。
// 非管理员即使库里残留 tenant 也降级为 custom（无名单时 self），避免越权。
func ResolveVisibility(scope string, visibleEmployeeIDs []int64, employeeID int64, isAdmin bool) *Visibility {
	if isAdmin {
		return &Visibility{Scope: DataScopeTenant, All: true, EmployeeIDs: []int64{}}
	}
	ids := NormalizeVisibleEmployeeIDs(visibleEmployeeIDs)
	switch NormalizeDataScope(scope) {
	case DataScopeTenant:
		if len(ids) > 0 {
			return &Visibility{Scope: DataScopeCustom, EmployeeIDs: ids}
		}
		return &Visibility{Scope: DataScopeSelf, EmployeeIDs: []int64{employeeID}}
	case DataScopeCustom:
		return &Visibility{Scope: DataScopeCustom, EmployeeIDs: ids}
	default:
		return &Visibility{Scope: DataScopeSelf, EmployeeIDs: []int64{employeeID}}
	}
}

// VisibilityEmployeeIDs 返回可直接用于 SQL 的参数：All 时为 nil（SQL 里视为不过滤）。
func (v *Visibility) VisibilityEmployeeIDs() []int64 {
	if v == nil || v.All {
		return nil
	}
	return v.EmployeeIDs
}
