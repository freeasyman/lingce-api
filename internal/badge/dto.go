package badge

// Device Management DTOs

// DeviceListRequest represents the request for listing devices
type DeviceListRequest struct {
	TenantID         *int64  `json:"tenant_id,omitempty"`
	TenantIDs        []int64 `json:"tenant_ids,omitempty"`
	EmployeeID       *int64  `json:"employee_id,omitempty"`
	Status           *string `json:"status,omitempty"`
	ManufacturerCode *string `json:"manufacturer_code,omitempty"`
	DeviceNo         *string `json:"device_no,omitempty"`
	Page             int     `json:"page"`
	PageSize         int     `json:"page_size"`
}

// DeviceResponse represents device response
type DeviceResponse struct {
	ID                 int64      `json:"id"`
	DeviceNo           string     `json:"device_no"`
	ManufacturerCode   string     `json:"manufacturer_code"`
	ManufacturerName   string     `json:"manufacturer_name"`
	HardwareModel      *string    `json:"hardware_model,omitempty"`
	Status             string     `json:"status"`
	TenantID           *int64     `json:"tenant_id,omitempty"`
	TenantName         *string    `json:"tenant_name,omitempty"`
	EmployeeID         *int64     `json:"employee_id,omitempty"`
	EmployeeName       *string    `json:"employee_name,omitempty"`
	AcceptedAt         *string    `json:"accepted_at,omitempty"`
	LastOnlineAt       *string    `json:"last_online_at,omitempty"`
	BatteryLevel       *int       `json:"battery_level,omitempty"`
	FirmwareVersion    *string    `json:"firmware_version,omitempty"`
	ExtraData          JSONObject `json:"extra_data,omitempty"`
	CreatedAt          string     `json:"created_at"`
	UpdatedAt          string     `json:"updated_at"`
}

// AcceptanceValidateRequest represents acceptance validation request
type AcceptanceValidateRequest struct {
	Devices []AcceptanceDeviceInput `json:"devices"`
}

// AcceptanceDeviceInput represents device input for acceptance
type AcceptanceDeviceInput struct {
	DeviceNo         string  `json:"device_no"`
	ManufacturerCode string  `json:"manufacturer_code"`
	HardwareModel    *string `json:"hardware_model,omitempty"`
}

// AcceptanceImportRequest represents acceptance import request
type AcceptanceImportRequest struct {
	Devices []AcceptanceDeviceInput `json:"devices"`
}

// AssignToTenantRequest represents assign to tenant request
type AssignToTenantRequest struct {
	DeviceIDs []int64 `json:"device_ids"`
	TenantID  int64   `json:"tenant_id"`
}

// AssignToEmployeeRequest represents assign to employee request
type AssignToEmployeeRequest struct {
	DeviceIDs  []int64 `json:"device_ids"`
	EmployeeID int64   `json:"employee_id"`
}

// ReclaimRequest represents reclaim request
type ReclaimRequest struct {
	DeviceIDs []int64 `json:"device_ids"`
	Notes     *string `json:"notes,omitempty"`
}

// TicketSubmitRequest represents ticket submit request
type TicketSubmitRequest struct {
	Type        string     `json:"type"`
	DeviceID    *int64     `json:"device_id,omitempty"`
	DeviceNo    *string    `json:"device_no,omitempty"`
	TenantID    *int64     `json:"-"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
}

// TicketListRequest represents ticket list request
type TicketListRequest struct {
	Type        *string `json:"type,omitempty"`
	Status      *string `json:"status,omitempty"`
	SubmitterID *int64  `json:"submitter_id,omitempty"`
	TenantID    *int64  `json:"tenant_id,omitempty"`
	Page        int     `json:"page"`
	PageSize    int     `json:"page_size"`
}

// TicketResponse represents ticket response
type TicketResponse struct {
	ID            int64      `json:"id"`
	TicketNo      string     `json:"ticket_no"`
	Type          string     `json:"type"`
	Status        string     `json:"status"`
	DeviceID      *int64     `json:"device_id,omitempty"`
	DeviceNo      *string    `json:"device_no,omitempty"`
	TenantID      *int64     `json:"tenant_id,omitempty"`
	TenantName    *string    `json:"tenant_name,omitempty"`
	EmployeeID    *int64     `json:"employee_id,omitempty"`
	EmployeeName  *string    `json:"employee_name,omitempty"`
	SubmitterID   int64      `json:"submitter_id"`
	SubmitterName string     `json:"submitter_name"`
	ReviewerID    *int64     `json:"reviewer_id,omitempty"`
	ReviewerName  *string    `json:"reviewer_name,omitempty"`
	ExecutorID    *int64     `json:"executor_id,omitempty"`
	ExecutorName  *string    `json:"executor_name,omitempty"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	ReviewNotes   *string    `json:"review_notes,omitempty"`
	ExecuteNotes  *string    `json:"execute_notes,omitempty"`
	SubmittedAt   string     `json:"submitted_at"`
	ReviewedAt    *string    `json:"reviewed_at,omitempty"`
	ExecutedAt    *string    `json:"executed_at,omitempty"`
	CompletedAt   *string    `json:"completed_at,omitempty"`
	ExtraData     JSONObject `json:"extra_data,omitempty"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
}

// TicketReviewRequest represents ticket review request
type TicketReviewRequest struct {
	Approved   bool    `json:"approved"`
	Notes      *string `json:"notes,omitempty"`
	Approve    *bool   `json:"approve,omitempty"`     // backward-compatible field
	ReviewNote *string `json:"review_note,omitempty"` // backward-compatible field
}

// TicketExecuteRequest represents ticket execute request
type TicketExecuteRequest struct {
	Notes         *string `json:"notes,omitempty"`
	Success       *bool   `json:"success,omitempty"`        // backward-compatible field
	ResultMessage *string `json:"result_message,omitempty"` // backward-compatible field
}

// DashboardSummaryResponse represents dashboard summary
type DashboardSummaryResponse struct {
	TotalDevices       int64               `json:"total_devices"`
	PendingAcceptance  int64               `json:"pending_acceptance"`
	PendingAssignment  int64               `json:"pending_assignment"`
	InUse              int64               `json:"in_use"`
	Maintenance        int64               `json:"maintenance"`
	Retired            int64               `json:"retired"`
	StatusDistribution []StatusCount       `json:"status_distribution"`
	ManufacturerStats  []ManufacturerCount `json:"manufacturer_stats"`
	TenantStats        []TenantDeviceCount `json:"tenant_stats"`
}

// StatusCount represents status count
type StatusCount struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

// ManufacturerCount represents manufacturer count
type ManufacturerCount struct {
	ManufacturerCode string `json:"manufacturer_code"`
	ManufacturerName string `json:"manufacturer_name"`
	Count            int64  `json:"count"`
}

// TenantDeviceCount represents tenant device count
type TenantDeviceCount struct {
	TenantID   int64  `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	Count      int64  `json:"count"`
}

// Smart Badge DTOs

// TenantDeviceListRequest represents tenant device list request
type TenantDeviceListRequest struct {
	TenantID   int64   `json:"tenant_id"`
	Status     *string `json:"status,omitempty"`
	EmployeeID *int64  `json:"employee_id,omitempty"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
}

// TenantDeviceOverviewResponse represents tenant device overview
type TenantDeviceOverviewResponse struct {
	TotalDevices   int64 `json:"total_devices"`
	InUse          int64 `json:"in_use"`
	Idle           int64 `json:"idle"`
	Maintenance    int64 `json:"maintenance"`
	OnlineDevices  int64 `json:"online_devices"`
	OfflineDevices int64 `json:"offline_devices"`
}

// RecordingControlRequest represents recording control request
type RecordingControlRequest struct {
	DeviceNo   string     `json:"device_no"`
	EmployeeID *int64     `json:"employee_id,omitempty"`
	ExtraData  JSONObject `json:"extra_data,omitempty"`
}

// RecordingControlLogListRequest represents recording control log list request
type RecordingControlLogListRequest struct {
	TenantID   *int64  `json:"tenant_id,omitempty"`
	EmployeeID *int64  `json:"employee_id,omitempty"`
	DeviceNo   *string `json:"device_no,omitempty"`
	Action     *string `json:"action,omitempty"`
	Status     *string `json:"status,omitempty"`
	StartDate  *string `json:"start_date,omitempty"`
	EndDate    *string `json:"end_date,omitempty"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
}

// RecordingControlLogResponse represents recording control log response
type RecordingControlLogResponse struct {
	ID           int64      `json:"id"`
	DeviceNo     string     `json:"device_no"`
	DeviceID     *int64     `json:"device_id,omitempty"`
	TenantID     *int64     `json:"tenant_id,omitempty"`
	TenantName   *string    `json:"tenant_name,omitempty"`
	EmployeeID   *int64     `json:"employee_id,omitempty"`
	EmployeeName *string    `json:"employee_name,omitempty"`
	Action       string     `json:"action"`
	Status       string     `json:"status"`
	OperatorID   *int64     `json:"operator_id,omitempty"`
	OperatorName *string    `json:"operator_name,omitempty"`
	ErrorMsg     *string    `json:"error_msg,omitempty"`
	ExtraData    JSONObject `json:"extra_data,omitempty"`
	CreatedAt    string     `json:"created_at"`
}

// CallbackPayload represents callback payload
type CallbackPayload struct {
	DeviceNo  string     `json:"device_no"`
	EventType string     `json:"event_type"`
	Data      JSONObject `json:"data"`
}

// MyBadgeStatusResponse represents my badge status
type MyBadgeStatusResponse struct {
	DeviceNo                 *string `json:"device_no,omitempty"`
	DeviceID                 *int64  `json:"device_id,omitempty"`
	Status                   *string `json:"status,omitempty"`
	IsOnline                 bool    `json:"is_online"`
	IsRecording              bool    `json:"is_recording"`
	RecordStatus             *int    `json:"record_status,omitempty"`
	RecordingDurationSeconds *int    `json:"recording_duration_seconds,omitempty"`
	BatteryLevel             *int    `json:"battery_level,omitempty"`
	FirmwareVersion          *string `json:"firmware_version,omitempty"`
	LastOnlineAt             *string `json:"last_online_at,omitempty"`
	WorkDays                 *int    `json:"work_days,omitempty"`
	TotalRecordings          *int    `json:"total_recordings,omitempty"`
	TotalCustomers           *int    `json:"total_customers,omitempty"`
}

// ManufacturerResponse represents manufacturer response
type ManufacturerResponse struct {
	ID            int64      `json:"id"`
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	ContactPerson *string    `json:"contact_person,omitempty"`
	ContactPhone  *string    `json:"contact_phone,omitempty"`
	ContactEmail  *string    `json:"contact_email,omitempty"`
	IsActive      bool       `json:"is_active"`
	Config        JSONObject `json:"config,omitempty"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
}

// UpdateManufacturerConfigRequest represents update manufacturer config request
type UpdateManufacturerConfigRequest struct {
	Config JSONObject `json:"config"`
}

// LifecycleLogResponse represents lifecycle log response
type LifecycleLogResponse struct {
	ID           int64      `json:"id"`
	DeviceID     int64      `json:"device_id"`
	Action       string     `json:"action"`
	FromStatus   *string    `json:"from_status,omitempty"`
	ToStatus     string     `json:"to_status"`
	TenantID     *int64     `json:"tenant_id,omitempty"`
	TenantName   *string    `json:"tenant_name,omitempty"`
	EmployeeID   *int64     `json:"employee_id,omitempty"`
	EmployeeName *string    `json:"employee_name,omitempty"`
	OperatorID   int64      `json:"operator_id"`
	OperatorName string     `json:"operator_name"`
	Notes        *string    `json:"notes,omitempty"`
	ExtraData    JSONObject `json:"extra_data,omitempty"`
	CreatedAt    string     `json:"created_at"`
}

// Recording Stats DTOs

// RecordingStatsResponse represents the complete recording statistics response
type RecordingStatsResponse struct {
	Summary     RecordingStatsSummary `json:"summary"`
	DailyTrends []DailyTrend          `json:"daily_trends"`
	ByDevice    []DeviceStats         `json:"by_device"`
	ByEmployee  []EmployeeStats       `json:"by_employee"`
}

// RecordingStatsSummary represents summary statistics
type RecordingStatsSummary struct {
	TotalRecordings              int `json:"total_recordings"`
	ActiveDeviceCount            int `json:"active_device_count"`
	TotalDeviceCount             int `json:"total_device_count"`
	ActiveEmployeeCount          int `json:"active_employee_count"`
	TotalEmployeeCount           int `json:"total_employee_count"`
	AvgDurationSeconds           int `json:"avg_duration_seconds"`
	PrevPeriodAvgDurationSeconds int `json:"prev_period_avg_duration_seconds"`
}

// DailyTrend represents daily recording trend
type DailyTrend struct {
	Date                 string `json:"date"`
	RecordingCount       int    `json:"recording_count"`
	ActiveDeviceCount    int    `json:"active_device_count"`
	TotalDurationSeconds int    `json:"total_duration_seconds"`
}

// DeviceStats represents per-device statistics
type DeviceStats struct {
	DeviceID             int64   `json:"device_id"`
	DeviceNo             string  `json:"device_no"`
	EmployeeID           *int64  `json:"employee_id,omitempty"`
	EmployeeName         *string `json:"employee_name,omitempty"`
	RecordingCount       int     `json:"recording_count"`
	TotalDurationSeconds int     `json:"total_duration_seconds"`
	LastRecordingAt      *string `json:"last_recording_at,omitempty"`
	IsOnline             bool    `json:"is_online"`
}

// EmployeeStats represents per-employee statistics
type EmployeeStats struct {
	EmployeeID      int64   `json:"employee_id"`
	EmployeeName    string  `json:"employee_name"`
	Department      *string `json:"department,omitempty"`
	DeviceID        *int64  `json:"device_id,omitempty"`
	DeviceNo        *string `json:"device_no,omitempty"`
	RecordingCount  int     `json:"recording_count"`
	RecordingDays   int     `json:"recording_days"`
	LastRecordingAt *string `json:"last_recording_at,omitempty"`
}
