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
	OrgID          *int64
	OrgName        string
	OrgType        string
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
	Name           string
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

// TenantOption is used when login requires tenant selection for a shared account.
type TenantOption struct {
	TenantID   int64  `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	IsActive   bool   `json:"is_active"`
}

// LoginRequestMeta carries request metadata used for institution system logs.
type LoginRequestMeta struct {
	IPAddress  string
	UserAgent  string
	DeviceType string
	RequestID  string
}

// InstitutionSystemActionLogInput represents a backend-created institution system log.
type InstitutionSystemActionLogInput struct {
	TenantID       *int64
	TenantName     string
	ActorID        *int64
	ActorName      string
	ActorRoleCode  string
	ActorRoleName  string
	LogType        string
	ActionCode     string
	ActionName     string
	RoutePath      string
	Result         string
	ErrorMessage   string
	ObjectType     string
	ObjectID       string
	ObjectName     string
	RequestSummary map[string]interface{}
	IPAddress      string
	UserAgent      string
	DeviceType     string
	RequestID      string
}
