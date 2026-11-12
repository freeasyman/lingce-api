package billing

// 运营端「机构管理员管理」的数据契约。
//
// 存储事实（与仓库现状一致）：
//   · 机构管理员 = employees 中拥有 institution_employee_roles(role_code=admin) 的员工；
//   · 主管理员 = employees.is_primary（迁移 20261112_011 新增，每租户最多一个有效主管理员）；
//   · 登录、角色、EMR 权限都沿用 employees / institution_employee_roles / institution_roles，
//     不另建账号体系（operations_admins 是运营端账号表，机构管理员不写那里）。
//
// JSON 字段按任务约定用 camelCase；初始密码属于敏感信息，任何响应都不返回。

import (
	"strings"
)

// 机构管理员角色（institution_roles.code）。
// 机构管理员管理接口只管理 admin 角色；其它机构角色（医生/咨询师等）走员工管理。
const institutionAdminRoleCode = "admin"

// institutionAdminRoleName 是 admin 角色缺失时的兜底展示名。
const institutionAdminRoleName = "管理员"

// InstitutionAdminAccount 是管理员列表/创建/修改接口的返回项。
type InstitutionAdminAccount struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	RoleCode    string `json:"roleCode"`
	IsPrimary   bool   `json:"isPrimary"`
	IsActive    bool   `json:"isActive"`
	LastLoginAt string `json:"lastLoginAt"`
}

// InstitutionAdminListResult 是 GET .../admins 的 data。
type InstitutionAdminListResult struct {
	Items []InstitutionAdminAccount `json:"items"`
}

// InstitutionAdminItemResult 是创建/修改/重置密码/设为主管理员的 data。
type InstitutionAdminItemResult struct {
	Item InstitutionAdminAccount `json:"item"`
}

// CreateInstitutionAdminRequest 是 POST .../admins 的请求体。
type CreateInstitutionAdminRequest struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// normalize 清洗并校验新增管理员入参。
func (r CreateInstitutionAdminRequest) normalize() (CreateInstitutionAdminRequest, error) {
	r.Name = strings.TrimSpace(r.Name)
	r.Phone = strings.TrimSpace(r.Phone)
	r.Email = strings.TrimSpace(r.Email)
	r.Role = strings.TrimSpace(r.Role)

	if r.Name == "" {
		return CreateInstitutionAdminRequest{}, invalidf("请填管理员姓名")
	}
	if len([]rune(r.Name)) > 100 {
		return CreateInstitutionAdminRequest{}, invalidf("管理员姓名过长")
	}
	if !mobilePhonePattern.MatchString(r.Phone) {
		return CreateInstitutionAdminRequest{}, invalidf("手机号格式不对")
	}
	if len([]rune(r.Email)) > 255 {
		return CreateInstitutionAdminRequest{}, invalidf("邮箱过长")
	}
	if r.Email != "" && !institutionContactEmailPattern.MatchString(r.Email) {
		return CreateInstitutionAdminRequest{}, invalidf("邮箱格式不对")
	}
	if len([]rune(r.Role)) > 64 {
		return CreateInstitutionAdminRequest{}, invalidf("角色过长")
	}
	if r.Role == "" {
		r.Role = institutionAdminRoleCode
	}
	return r, nil
}

// UpdateInstitutionAdminRequest 是 PUT .../admins/{admin_id} 的请求体。
// 只允许改基本信息与启停；手机号/角色/主管理员分别由各自的接口负责。
type UpdateInstitutionAdminRequest struct {
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	IsActive *bool   `json:"isActive"`
}

// normalize 清洗并校验修改管理员入参。
func (r UpdateInstitutionAdminRequest) normalize() (UpdateInstitutionAdminRequest, error) {
	if r.Name != nil {
		trimmed := strings.TrimSpace(*r.Name)
		if trimmed == "" {
			return UpdateInstitutionAdminRequest{}, invalidf("请填管理员姓名")
		}
		if len([]rune(trimmed)) > 100 {
			return UpdateInstitutionAdminRequest{}, invalidf("管理员姓名过长")
		}
		r.Name = &trimmed
	}
	if r.Email != nil {
		trimmed := strings.TrimSpace(*r.Email)
		if len([]rune(trimmed)) > 255 {
			return UpdateInstitutionAdminRequest{}, invalidf("邮箱过长")
		}
		if trimmed != "" && !institutionContactEmailPattern.MatchString(trimmed) {
			return UpdateInstitutionAdminRequest{}, invalidf("邮箱格式不对")
		}
		r.Email = &trimmed
	}
	return r, nil
}
