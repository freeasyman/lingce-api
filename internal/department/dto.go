package department

// CreateDepartmentRequest represents a request to create a department
type CreateDepartmentRequest struct {
	TenantID int64  `json:"tenant_id"`
	Name     string `json:"name"`
	Code     string `json:"code"`
	ParentID *int64 `json:"parent_id,omitempty"`
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
	ID        int64  `json:"id"`
	TenantID  int64  `json:"tenant_id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	ParentID  *int64 `json:"parent_id,omitempty"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
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
