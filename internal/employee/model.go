package employee

import "time"

// Employee represents an employee within a tenant
type Employee struct {
	ID             int64      `json:"id"`
	TenantID       int64      `json:"tenant_id"`
	Username       string     `json:"username"`
	PasswordHash   string     `json:"-"`
	FullName       string     `json:"full_name"`
	Phone          string     `json:"phone"`
	Email          string     `json:"email"`
	DepartmentID   *int64     `json:"department_id,omitempty"`
	RoleCode       string     `json:"role_code,omitempty"`
	RoleName       string     `json:"role,omitempty"`
	SessionVersion int        `json:"session_version"`
	IsActive       bool       `json:"is_active"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}
