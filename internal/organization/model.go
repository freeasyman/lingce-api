package organization

import "time"

// Tenant represents a tenant/organization
type Tenant struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	IsActive  bool       `json:"is_active"`
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// MedicalSpecialty represents a medical specialty
type MedicalSpecialty struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	ParentID  *int64 `json:"parent_id,omitempty"`
	Level     int    `json:"level"`
	SortOrder int    `json:"sort_order"`
}

// EmployeeAssistantAssignment represents the assistant binding relationship
type EmployeeAssistantAssignment struct {
	ID          int64     `json:"id"`
	EmployeeID  int64     `json:"employee_id"`
	AssistantID int64     `json:"assistant_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// InstitutionStatistics represents institution statistics
type InstitutionStatistics struct {
	TotalTenants       int `json:"total_tenants"`
	ActiveTenants      int `json:"active_tenants"`
	TotalEmployees     int `json:"total_employees"`
	TotalDepartments   int `json:"total_departments"`
	TotalBadgeDevices  int `json:"total_badge_devices"`
	TotalRecordings    int `json:"total_recordings"`
	RecordingsThisWeek int `json:"recordings_this_week"`
}

// Doctor represents doctor-facing projection over employees.
type Doctor struct {
	ID           int64      `json:"id"`
	TenantID     int64      `json:"tenant_id"`
	DepartmentID *int64     `json:"department_id,omitempty"`
	Name         string     `json:"name"`
	Phone        string     `json:"phone"`
	Email        string     `json:"email"`
	IsActive     bool       `json:"is_active"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Patient represents patient-facing projection over customers.
type Patient struct {
	ID         int64      `json:"id"`
	TenantID   int64      `json:"tenant_id"`
	Name       string     `json:"name"`
	Phone      *string    `json:"phone,omitempty"`
	Email      *string    `json:"email,omitempty"`
	Gender     *string    `json:"gender,omitempty"`
	Age        *int       `json:"age,omitempty"`
	Status     string     `json:"status"`
	Momentum   int        `json:"momentum"`
	AssignedTo *int64     `json:"assigned_to,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
