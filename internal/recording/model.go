package recording

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// RecordingStatus represents the status of a medical recording
type RecordingStatus string

const (
	StatusPending    RecordingStatus = "pending"
	StatusProcessing RecordingStatus = "processing"
	StatusCompleted  RecordingStatus = "completed"
	StatusFailed     RecordingStatus = "failed"
)

// RecordingScene represents the scene type of a recording
type RecordingScene string

const (
	SceneConsultation RecordingScene = "consultation" // 咨询
	SceneDiagnosis    RecordingScene = "diagnosis"    // 诊断
	SceneTreatment    RecordingScene = "treatment"    // 治疗
	SceneFollowUp     RecordingScene = "follow_up"    // 跟进
)

// RecordingSource represents the source of a recording
type RecordingSource string

const (
	SourceBadge  RecordingSource = "badge"  // 智能工牌
	SourceManual RecordingSource = "manual" // 手动上传
	SourceApp    RecordingSource = "app"    // 移动应用
)

// RecordingScope represents listing scope for role-based recording separation.
type RecordingScope string

const (
	RecordingScopeDoctor     RecordingScope = "doctor"
	RecordingScopeConsultant RecordingScope = "consultant"
)

// TaskStatus represents the status of a recording task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusAssigned  TaskStatus = "assigned"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// TaskType represents the type of a recording task
type TaskType string

const (
	TaskTypeFollowUp TaskType = "follow_up" // 跟进任务
	TaskTypeReview   TaskType = "review"    // 复查任务
	TaskTypeCallback TaskType = "callback"  // 回访任务
)

// MedicalRecording represents a medical consultation recording
type MedicalRecording struct {
	ID                 int64            `json:"id"`
	TenantID           int64            `json:"tenant_id"`
	TenantName         string           `json:"tenant_name"`
	EmployeeID         int64            `json:"employee_id"`
	EmployeeName       string           `json:"employee_name"`
	CustomerID         *int64           `json:"customer_id,omitempty"`
	CustomerName       *string          `json:"customer_name,omitempty"`
	PatientName        string           `json:"patient_name"`
	PatientAge         *int             `json:"patient_age,omitempty"`
	PatientGender      *string          `json:"patient_gender,omitempty"`
	PatientPhone       *string          `json:"patient_phone,omitempty"`
	RecordingURL       string           `json:"recording_url"`
	RecordingDuration  *int             `json:"recording_duration,omitempty"`
	Scene              *RecordingScene  `json:"scene,omitempty"`
	Source             *RecordingSource `json:"source,omitempty"`
	TranscriptText     *string          `json:"transcript_text,omitempty"`
	DoctorSummary      *string          `json:"doctor_summary,omitempty"`
	TherapistSummary   *string          `json:"therapist_summary,omitempty"`
	ConsultantSummary  *string          `json:"consultant_summary,omitempty"`
	AnalysisDisplay    JSONObject       `json:"analysis_display,omitempty"`
	Status             RecordingStatus  `json:"status"`
	ProcessingError    *string          `json:"processing_error,omitempty"`
	RecordingStartedAt *time.Time       `json:"recording_started_at,omitempty"`
	RecordingEndedAt   *time.Time       `json:"recording_ended_at,omitempty"`
	ProcessedAt        *time.Time       `json:"processed_at,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	DeletedAt          *time.Time       `json:"deleted_at,omitempty"`
}

// RecordingTask represents a task generated from a recording
type RecordingTask struct {
	ID            int64      `json:"id"`
	TenantID      int64      `json:"tenant_id"`
	RecordingID   int64      `json:"recording_id"`
	TaskType      TaskType   `json:"task_type"`
	Title         string     `json:"title"`
	Description   *string    `json:"description,omitempty"`
	CustomerName  *string    `json:"customer_name,omitempty"`
	Priority      *string    `json:"priority,omitempty"`
	Script        *string    `json:"script,omitempty"`
	ContactReason *string    `json:"contact_reason,omitempty"`
	SourceType    *string    `json:"source_type,omitempty"`
	SourceDetail  *string    `json:"source_detail,omitempty"`
	AssignedTo    *int64     `json:"assigned_to,omitempty"`
	AssignedBy    *int64     `json:"assigned_by,omitempty"`
	Status        TaskStatus `json:"status"`
	DueDate       *time.Time `json:"due_date,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CompletedBy   *int64     `json:"completed_by,omitempty"`
	CancelledAt   *time.Time `json:"cancelled_at,omitempty"`
	CancelReason  *string    `json:"cancel_reason,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// RecordingAnalysisResult represents the analysis result of a recording
type RecordingAnalysisResult struct {
	ID                   int64     `json:"id"`
	RecordingID          int64     `json:"recording_id"`
	TenantID             int64     `json:"tenant_id"`
	TranscriptText       *string   `json:"transcript_text,omitempty"`
	CleanedText          *string   `json:"cleaned_text,omitempty"`
	DoctorSummary        *string   `json:"doctor_summary,omitempty"`
	TherapistSummary     *string   `json:"therapist_summary,omitempty"`
	ConsultantSummary    *string   `json:"consultant_summary,omitempty"`
	KeyPoints            JSONArray `json:"key_points,omitempty"`
	Concerns             JSONArray `json:"concerns,omitempty"`
	Recommendations      JSONArray `json:"recommendations,omitempty"`
	SentimentScore       *float64  `json:"sentiment_score,omitempty"`
	CommunicationScore   *float64  `json:"communication_score,omitempty"`
	ProfessionalismScore *float64  `json:"professionalism_score,omitempty"`
	FollowUpSuggestions  JSONArray `json:"follow_up_suggestions,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// RecordingPrompt represents a prompt template for recording analysis
type RecordingPrompt struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	PromptText  string     `json:"prompt_text"`
	Variables   JSONArray  `json:"variables,omitempty"`
	IsActive    bool       `json:"is_active"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// RecordingPromptTenantConfig represents tenant-specific prompt configuration
type RecordingPromptTenantConfig struct {
	ID         int64      `json:"id"`
	TenantID   int64      `json:"tenant_id"`
	PromptCode string     `json:"prompt_code"`
	PromptText string     `json:"prompt_text"`
	IsActive   bool       `json:"is_active"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
}

// RecordingBestPractice represents a best practice example from recordings
type RecordingBestPractice struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	RecordingID int64      `json:"recording_id"`
	Title       string     `json:"title"`
	Description *string    `json:"description,omitempty"`
	Category    *string    `json:"category,omitempty"`
	Tags        JSONArray  `json:"tags,omitempty"`
	CreatedBy   int64      `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// EmployeePartnership represents a partnership between employees
type EmployeePartnership struct {
	ID           int64      `json:"id"`
	TenantID     int64      `json:"tenant_id"`
	EmployeeID   int64      `json:"employee_id"`
	PartnerID    int64      `json:"partner_id"`
	Relationship string     `json:"relationship"` // doctor-assistant, mentor-mentee, etc.
	CreatedAt    time.Time  `json:"created_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

// RecordingInstitutionRuleConfig represents institution-specific rule configuration
type RecordingInstitutionRuleConfig struct {
	ID         int64      `json:"id"`
	TenantID   int64      `json:"tenant_id"`
	RuleType   string     `json:"rule_type"`
	RuleConfig JSONObject `json:"rule_config"`
	IsActive   bool       `json:"is_active"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
}

// JSONArray is a custom type for JSON array fields
type JSONArray []interface{}

// Scan implements the sql.Scanner interface
func (j *JSONArray) Scan(value interface{}) error {
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

// Value implements the driver.Valuer interface
func (j JSONArray) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// JSONObject is a custom type for JSON object fields
type JSONObject map[string]interface{}

// Scan implements the sql.Scanner interface
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

// Value implements the driver.Valuer interface
func (j JSONObject) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}
