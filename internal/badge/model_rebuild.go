package badge

import "time"

const (
	BadgeStatusPendingAcceptance = "pending_acceptance"
	BadgeStatusInStock           = "in_stock"
	BadgeStatusAssigned          = "assigned"
	BadgeStatusReturned          = "returned"
	BadgeStatusUnusable          = "unusable"
	BadgeStatusRetired           = "retired"
)

const (
	BadgeHealthUnknown = "unknown"
	BadgeHealthHealthy = "healthy"
	BadgeHealthWarning = "warning"
	BadgeHealthError   = "error"
)

type BadgeDeviceV2 struct {
	ID                   int64      `json:"id"`
	DeviceNo             string     `json:"device_no"`
	ManufacturerID       *int64     `json:"manufacturer_id,omitempty"`
	ManufacturerCode     string     `json:"manufacturer_code"`
	ManufacturerName     *string    `json:"manufacturer_name,omitempty"`
	AppID                *string    `json:"app_id,omitempty"`
	DeviceUID            *string    `json:"device_uid,omitempty"`
	HardwareModel        *string    `json:"hardware_model,omitempty"`
	BadgeStatus          string     `json:"badge_status"`
	CurrentTenantID      *int64     `json:"current_tenant_id,omitempty"`
	CurrentTenantName    *string    `json:"current_tenant_name,omitempty"`
	CurrentEmployeeID    *int64     `json:"current_employee_id,omitempty"`
	CurrentEmployeeName  *string    `json:"current_employee_name,omitempty"`
	CurrentEmployeePhone *string    `json:"current_employee_phone,omitempty"`
	AssignedAt           *time.Time `json:"assigned_at,omitempty"`
	HealthLevel          string     `json:"health_level"`
	BatteryLevel         *int       `json:"battery_level,omitempty"`
	LastOnlineAt         *time.Time `json:"last_online_at,omitempty"`
	LastHealthCheckAt    *time.Time `json:"last_health_check_at,omitempty"`
	HealthCheckResult    JSONObject `json:"health_check_result,omitempty"`
	ImportBatchNo        *string    `json:"import_batch_no,omitempty"`
	AcceptanceBatchNo    *string    `json:"acceptance_batch_no,omitempty"`
	AcceptedAt           *time.Time `json:"accepted_at,omitempty"`
	AcceptanceResult     *string    `json:"acceptance_result,omitempty"`
	Metadata             JSONObject `json:"metadata,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	DeletedAt            *time.Time `json:"deleted_at,omitempty"`
}

type BadgeAssignmentLogV2 struct {
	ID            int64     `json:"id"`
	BadgeDeviceID int64     `json:"badge_device_id"`
	DeviceNo      string    `json:"device_no"`
	TenantID      *int64    `json:"tenant_id,omitempty"`
	TenantName    *string   `json:"tenant_name,omitempty"`
	EmployeeID    *int64    `json:"employee_id,omitempty"`
	EmployeeName  *string   `json:"employee_name,omitempty"`
	Action        string    `json:"action"`
	Reason        *string   `json:"reason,omitempty"`
	OperatorID    *int64    `json:"operator_id,omitempty"`
	OperatorName  *string   `json:"operator_name,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type BadgeTicketV2 struct {
	ID                int64      `json:"id"`
	TicketNo          string     `json:"ticket_no"`
	BadgeDeviceID     int64      `json:"badge_device_id"`
	DeviceNo          string     `json:"device_no"`
	TicketType        string     `json:"ticket_type"`
	TicketStatus      string     `json:"ticket_status"`
	TargetBadgeStatus *string    `json:"target_badge_status,omitempty"`
	TenantID          *int64     `json:"tenant_id,omitempty"`
	EmployeeID        *int64     `json:"employee_id,omitempty"`
	SubmitterID       int64      `json:"submitter_id"`
	ReviewerID        *int64     `json:"reviewer_id,omitempty"`
	ExecutorID        *int64     `json:"executor_id,omitempty"`
	Notes             JSONObject `json:"notes,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type BadgeDeviceLogV2 struct {
	ID              int64      `json:"id"`
	BadgeDeviceID   int64      `json:"badge_device_id"`
	DeviceNo        string     `json:"device_no"`
	Operation       string     `json:"operation"`
	FromBadgeStatus *string    `json:"from_badge_status,omitempty"`
	ToBadgeStatus   *string    `json:"to_badge_status,omitempty"`
	OperatorID      *int64     `json:"operator_id,omitempty"`
	OperatorName    *string    `json:"operator_name,omitempty"`
	OperatorType    *string    `json:"operator_type,omitempty"`
	Detail          JSONObject `json:"detail,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}
