package badge

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// BadgeDevice represents a badge device
type BadgeDevice struct {
	ID                 int64      `json:"id"`
	DeviceNo           string     `json:"device_no"`
	DeviceID           *string    `json:"device_id,omitempty"`
	ManufacturerCode   string     `json:"manufacturer_code"`
	ManufacturerName   *string    `json:"manufacturer_name,omitempty"`
	Model              *string    `json:"model,omitempty"`
	HardwareModel      *string    `json:"hardware_model,omitempty"`
	Status             string     `json:"status"` // "pending_acceptance", "pending_assignment", "in_use", "maintenance", "retired"
	HealthStatus       string     `json:"health_status"`
	HealthCheckResult  JSONObject `json:"health_check_result,omitempty"`
	TenantID           *int64     `json:"tenant_id,omitempty"`
	TenantName         *string    `json:"tenant_name,omitempty"`
	EmployeeID         *int64     `json:"employee_id,omitempty"`
	EmployeeName       *string    `json:"employee_name,omitempty"`
	EmployeePhone      *string    `json:"employee_phone,omitempty"`
	AssignedAt         *time.Time `json:"assigned_at,omitempty"`
	AcceptedAt         *time.Time `json:"accepted_at,omitempty"`
	AssignedToTenantAt *time.Time `json:"assigned_to_tenant_at,omitempty"`
	AssignedToEmpAt    *time.Time `json:"assigned_to_emp_at,omitempty"`
	LastOnlineAt       *time.Time `json:"last_online_at,omitempty"`
	BatteryLevel       *int       `json:"battery_level,omitempty"`
	LastCheckAt        *time.Time `json:"last_check_at,omitempty"`
	FirmwareVersion    *string    `json:"firmware_version,omitempty"`
	ImportBatchNo      *string    `json:"import_batch_no,omitempty"`
	Metadata           JSONObject `json:"metadata,omitempty"`
	ExtraData          JSONObject `json:"extra_data,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
}

// BadgeDeviceLog represents v2 device operation logs
type BadgeDeviceLog struct {
	ID           int64      `json:"id"`
	DeviceID     int64      `json:"device_id"`
	DeviceNo     string     `json:"device_no"`
	Operation    string     `json:"operation"`
	FromStatus   *string    `json:"from_status,omitempty"`
	ToStatus     *string    `json:"to_status,omitempty"`
	OperatorID   *int64     `json:"operator_id,omitempty"`
	OperatorName *string    `json:"operator_name,omitempty"`
	OperatorType *string    `json:"operator_type,omitempty"`
	Detail       JSONObject `json:"detail,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// BadgeDeviceLifecycleLog represents device lifecycle log
type BadgeDeviceLifecycleLog struct {
	ID         int64      `json:"id"`
	DeviceID   int64      `json:"device_id"`
	Action     string     `json:"action"` // "acceptance", "assign_tenant", "assign_employee", "reclaim", "maintenance", "retire"
	FromStatus *string    `json:"from_status,omitempty"`
	ToStatus   string     `json:"to_status"`
	TenantID   *int64     `json:"tenant_id,omitempty"`
	EmployeeID *int64     `json:"employee_id,omitempty"`
	OperatorID int64      `json:"operator_id"`
	Notes      *string    `json:"notes,omitempty"`
	ExtraData  JSONObject `json:"extra_data,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// BadgeTicket represents a badge ticket
type BadgeTicket struct {
	ID           int64      `json:"id"`
	TicketNo     string     `json:"ticket_no"`
	Type         string     `json:"type"`   // "maintenance", "replacement", "reclaim", "exception"
	Status       string     `json:"status"` // "pending", "approved", "rejected", "executing", "completed", "cancelled"
	DeviceID     *int64     `json:"device_id,omitempty"`
	DeviceNo     *string    `json:"device_no,omitempty"`
	TenantID     *int64     `json:"tenant_id,omitempty"`
	EmployeeID   *int64     `json:"employee_id,omitempty"`
	SubmitterID  int64      `json:"submitter_id"`
	ReviewerID   *int64     `json:"reviewer_id,omitempty"`
	ExecutorID   *int64     `json:"executor_id,omitempty"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	ReviewNotes  *string    `json:"review_notes,omitempty"`
	ExecuteNotes *string    `json:"execute_notes,omitempty"`
	SubmittedAt  time.Time  `json:"submitted_at"`
	ReviewedAt   *time.Time `json:"reviewed_at,omitempty"`
	ExecutedAt   *time.Time `json:"executed_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ExtraData    JSONObject `json:"extra_data,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// BadgeManufacturer represents a badge manufacturer
type BadgeManufacturer struct {
	ID            int64      `json:"id"`
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	ContactPerson *string    `json:"contact_person,omitempty"`
	ContactPhone  *string    `json:"contact_phone,omitempty"`
	ContactEmail  *string    `json:"contact_email,omitempty"`
	APIEndpoint   *string    `json:"api_endpoint,omitempty"`
	APIKey        *string    `json:"api_key,omitempty"`
	IsActive      bool       `json:"is_active"`
	Config        JSONObject `json:"config,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// BadgeRecordingControlLog represents recording control log
type BadgeRecordingControlLog struct {
	ID         int64      `json:"id"`
	DeviceNo   string     `json:"device_no"`
	DeviceID   *int64     `json:"device_id,omitempty"`
	TenantID   *int64     `json:"tenant_id,omitempty"`
	EmployeeID *int64     `json:"employee_id,omitempty"`
	Action     string     `json:"action"` // "start", "stop"
	Status     string     `json:"status"` // "success", "failed"
	OperatorID *int64     `json:"operator_id,omitempty"`
	ErrorMsg   *string    `json:"error_msg,omitempty"`
	ExtraData  JSONObject `json:"extra_data,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// BadgePendingEvent represents pending event
type BadgePendingEvent struct {
	ID          int64      `json:"id"`
	EventType   string     `json:"event_type"` // "audio_callback", "developer_callback"
	DeviceNo    string     `json:"device_no"`
	Payload     JSONObject `json:"payload"`
	Status      string     `json:"status"` // "pending", "processing", "completed", "failed"
	RetryCount  int        `json:"retry_count"`
	ErrorMsg    *string    `json:"error_msg,omitempty"`
	ProcessedAt *time.Time `json:"processed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// BadgeVendorPoolDevice represents vendor pool device
type BadgeVendorPoolDevice struct {
	ID               int64      `json:"id"`
	DeviceNo         string     `json:"device_no"`
	ManufacturerCode string     `json:"manufacturer_code"`
	Status           string     `json:"status"` // "in_pool", "accepted", "exception"
	Model            *string    `json:"model,omitempty"`
	FirmwareVersion  *string    `json:"firmware_version,omitempty"`
	LastSyncAt       *time.Time `json:"last_sync_at,omitempty"`
	ExtraData        JSONObject `json:"extra_data,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// BadgeVendorPoolSyncBatch represents sync batch
type BadgeVendorPoolSyncBatch struct {
	ID               int64     `json:"id"`
	ManufacturerCode string    `json:"manufacturer_code"`
	TotalCount       int       `json:"total_count"`
	NewCount         int       `json:"new_count"`
	UpdatedCount     int       `json:"updated_count"`
	ExceptionCount   int       `json:"exception_count"`
	Status           string    `json:"status"` // "completed", "partial", "failed"
	SyncedBy         int64     `json:"synced_by"`
	SyncedAt         time.Time `json:"synced_at"`
	CreatedAt        time.Time `json:"created_at"`
}

// Custom JSON type for database storage
type JSONObject map[string]interface{}

func (j *JSONObject) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, j)
}

func (j JSONObject) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}
