package auth

import "time"

// OperationsAdmin represents an operations admin user
type OperationsAdmin struct {
	ID             int64
	Username       string
	PasswordHash   string
	RealName       string
	Email          string
	Phone          string
	SessionVersion int
	IsActive       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

// Employee represents an institution employee
type Employee struct {
	ID             int64
	TenantID       int64
	Username       string
	PasswordHash   string
	FullName       string
	Phone          string
	Email          string
	DepartmentID   *int64
	SessionVersion int
	IsActive       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

// Tenant represents a tenant/institution
type Tenant struct {
	ID        int64
	Name      string
	Code      string
	IsActive  bool
	ValidFrom *time.Time
	ValidTo   *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// SMSLoginCode represents a SMS verification code
type SMSLoginCode struct {
	ID        int64
	Phone     string
	Code      string
	ExpiresAt time.Time
	Used      bool
	CreatedAt time.Time
}
