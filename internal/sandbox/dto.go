package sandbox

import "time"

type SearchRequest struct {
	SourceTenantID int64
	DateFrom       *time.Time
	DateTo         *time.Time
	Keyword        string
	TranscriptQ    string
	EmployeeIDs    []int64
	IncludeShort   bool
	Page           int
	PageSize       int
}

type SearchItem struct {
	ID              int64      `json:"id"`
	TenantID        int64      `json:"tenant_id"`
	TenantName      string     `json:"tenant_name"`
	EmployeeID      int64      `json:"employee_id"`
	EmployeeName    string     `json:"employee_name"`
	EmployeeRole    string     `json:"employee_role,omitempty"`
	PatientName     string     `json:"patient_name"`
	DurationSeconds int        `json:"duration_seconds"`
	RecordedAt      *time.Time `json:"recorded_at"`
	AnalysisStatus  string     `json:"analysis_status"`
}

type EstimateResponse struct {
	TotalCount           int   `json:"total_count"`
	TotalDurationSeconds int64 `json:"total_duration_seconds"`
	TotalSizeBytes       int64 `json:"total_size_bytes"`
	ETASeconds           int64 `json:"eta_seconds"`
}

type CreateTaskRequest struct {
	SourceTenantID     int64                  `json:"source_tenant_id"`
	TargetTenantID     int64                  `json:"target_tenant_id"`
	RecordingIDs       []int64                `json:"recording_ids"`
	FilterSnapshot     map[string]interface{} `json:"filter_snapshot"`
	AssignmentMode     string                 `json:"assignment_mode"`
	TargetEmployeeIDs  []int64                `json:"target_employee_ids"`
	TargetEmployeeWgts []int                  `json:"target_employee_weights"`
	UseEmployeeMapping bool                   `json:"use_employee_mapping"`
	TTLDays            int                    `json:"ttl_days"`
}

type Task struct {
	ID               int64      `json:"id"`
	TaskNo           string     `json:"task_no"`
	SourceTenantID   int64      `json:"source_tenant_id"`
	SourceTenantName string     `json:"source_tenant_name,omitempty"`
	TargetTenantID   int64      `json:"target_tenant_id"`
	TargetTenantName string     `json:"target_tenant_name,omitempty"`
	AssignmentMode   string     `json:"assignment_mode"`
	Status           string     `json:"status"`
	TotalCount       int        `json:"total_count"`
	SuccessCount     int        `json:"success_count"`
	FailedCount      int        `json:"failed_count"`
	CreatedBy        int64      `json:"created_by"`
	CreatedByName    string     `json:"created_by_name,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
}

type TaskItem struct {
	ID                 int64      `json:"id"`
	TaskID             int64      `json:"task_id"`
	SourceRecordingID  int64      `json:"source_recording_id"`
	TargetRecordingID  *int64     `json:"target_recording_id,omitempty"`
	SourceObjectKey    string     `json:"source_object_key,omitempty"`
	TargetObjectKey    string     `json:"target_object_key,omitempty"`
	TargetEmployeeID   *int64     `json:"target_employee_id,omitempty"`
	TargetEmployeeName string     `json:"target_employee_name,omitempty"`
	Status             string     `json:"status"`
	ErrorCode          string     `json:"error_code,omitempty"`
	ErrorMessage       string     `json:"error_message,omitempty"`
	CopiedAt           *time.Time `json:"copied_at,omitempty"`
}

type EmployeeMapping struct {
	ID                 int64     `json:"id"`
	SourceTenantID     int64     `json:"source_tenant_id"`
	SourceEmployeeID   int64     `json:"source_employee_id"`
	SourceEmployeeName string    `json:"source_employee_name,omitempty"`
	TargetTenantID     int64     `json:"target_tenant_id"`
	TargetEmployeeID   int64     `json:"target_employee_id"`
	TargetEmployeeName string    `json:"target_employee_name,omitempty"`
	IsActive           bool      `json:"is_active"`
	CreatedAt          time.Time `json:"created_at"`
}
