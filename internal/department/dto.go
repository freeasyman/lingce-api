package department

// CreateDepartmentRequest represents a request to create a department
type CreateDepartmentRequest struct {
	TenantID int64  `json:"tenant_id"`
	Name     string `json:"name"`
	Code     string `json:"code"`
	DictCode string `json:"dict_code,omitempty"` // 标准科室：字典编码；为空表示自定义
	Type     string `json:"type,omitempty"`      // clinical / functional
	IsCustom bool   `json:"is_custom,omitempty"`
	ParentID *int64 `json:"parent_id,omitempty"`
}

// DepartmentDictItem 平台组织字典项（科室/职能部）。
type DepartmentDictItem struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// DepartmentCurrentTemplate 科室当前自动匹配的模板（只读，供机构端展示）。
type DepartmentCurrentTemplate struct {
	ID                      int64  `json:"id"`
	Name                    string `json:"name"`
	Code                    string `json:"code"`
	FirstVisitTemplateName  string `json:"first_visit_template_name"`
	FirstVisitScope         string `json:"first_visit_scope"`
	FirstVisitFallback      bool   `json:"first_visit_fallback"`
	FollowVisitTemplateName string `json:"follow_visit_template_name"`
	FollowVisitScope        string `json:"follow_visit_scope"`
	FollowVisitFallback     bool   `json:"follow_visit_fallback"`
}

// UpdateDepartmentRequest represents a request to update a department
type UpdateDepartmentRequest struct {
	Name     *string `json:"name,omitempty"`
	Code     *string `json:"code,omitempty"`
	ParentID *int64  `json:"parent_id,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
}

// DepartmentResponse represents a department response
type DepartmentResponse struct {
	ID              int64   `json:"id"`
	TenantID        int64   `json:"tenant_id"`
	Name            string  `json:"name"`
	Code            string  `json:"code"`
	ParentID        *int64  `json:"parent_id,omitempty"`
	IsActive        bool    `json:"is_active"`
	Type            string  `json:"type,omitempty"`
	IsCustom        bool    `json:"is_custom"`
	DefaultRoleCode *string `json:"default_role_code,omitempty"`
	DefaultRoleName *string `json:"default_role_name,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// DepartmentListRequest represents a request to list departments
type DepartmentListRequest struct {
	TenantID int64  `json:"tenant_id"`
	Name     string `json:"name,omitempty"`
	Code     string `json:"code,omitempty"`
	ParentID *int64 `json:"parent_id,omitempty"`
	IsActive *bool  `json:"is_active,omitempty"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
