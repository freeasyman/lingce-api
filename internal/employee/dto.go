package employee

// CreateEmployeeRequest represents a request to create an employee
type CreateEmployeeRequest struct {
	TenantID     int64  `json:"tenant_id"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	FullName     string `json:"full_name"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
	DepartmentID *int64 `json:"department_id,omitempty"`
}

// UpdateEmployeeRequest represents a request to update an employee
type UpdateEmployeeRequest struct {
	FullName     *string `json:"full_name,omitempty"`
	Phone        *string `json:"phone,omitempty"`
	Email        *string `json:"email,omitempty"`
	DepartmentID *int64  `json:"department_id,omitempty"`
	IsActive     *bool   `json:"is_active,omitempty"`
}

// ResetPasswordRequest represents a request to reset employee password
type ResetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// EmployeeResponse represents an employee response
type EmployeeResponse struct {
	ID             int64  `json:"id"`
	TenantID       int64  `json:"tenant_id"`
	Username       string `json:"username"`
	FullName       string `json:"full_name"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	DepartmentID   *int64 `json:"department_id,omitempty"`
	SessionVersion int    `json:"session_version"`
	IsActive       bool   `json:"is_active"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// EmployeeListRequest represents a request to list employees
type EmployeeListRequest struct {
	TenantID     int64  `json:"tenant_id"`
	Username     string `json:"username,omitempty"`
	FullName     string `json:"full_name,omitempty"`
	Phone        string `json:"phone,omitempty"`
	DepartmentID *int64 `json:"department_id,omitempty"`
	IsActive     *bool  `json:"is_active,omitempty"`
	Page         int    `json:"page"`
	PageSize     int    `json:"page_size"`
}
