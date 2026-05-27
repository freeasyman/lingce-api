package recording

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
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
	RecordingScopeFrontdesk  RecordingScope = "frontdesk"
	RecordingScopeTherapist  RecordingScope = "therapist"
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
	DepartmentName     string           `json:"department_name,omitempty"`
	DeviceNo           string           `json:"device_no,omitempty"`
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
	BusinessScope      string           `json:"business_scope,omitempty"`
	AnalysisResult     JSONObject       `json:"analysis_result,omitempty"`
	AnalysisDisplay    JSONObject       `json:"analysis_display,omitempty"`
	AnalysisStatus     *string          `json:"analysis_status,omitempty"`
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
	ID           int64      `json:"id"`
	Code         string     `json:"code"`
	Name         string     `json:"name"`
	Description  *string    `json:"description,omitempty"`
	Category     string     `json:"category"`
	SystemPrompt string     `json:"system_prompt"`
	PromptText   string     `json:"prompt_text"`
	OutputSchema JSONObject `json:"output_schema,omitempty"`
	Version      string     `json:"version"`
	Variables    JSONArray  `json:"variables,omitempty"`
	IsActive     bool       `json:"is_active"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
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

	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return nil
	}

	// Normal path: JSON object payload.
	if err := json.Unmarshal(raw, j); err == nil {
		return nil
	}

	// Backward-compat path: DB stored a JSON string whose content is another JSON object.
	var nested string
	if err := json.Unmarshal(raw, &nested); err != nil {
		return err
	}
	nested = strings.TrimSpace(nested)
	if nested == "" {
		*j = nil
		return nil
	}
	return json.Unmarshal([]byte(nested), j)
}

// Value implements the driver.Valuer interface
func (j JSONObject) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// ===== Lingce Sales Models =====

// DecisionStage represents the pipeline stage of a sales prospect
type DecisionStage string

const (
	StageFirstContact  DecisionStage = "first_contact"
	StageNeedConfirmed DecisionStage = "need_confirmed"
	StageLogicShifted  DecisionStage = "logic_shifted"
	StageEvaluating    DecisionStage = "evaluating"
	StageReadyToClose  DecisionStage = "ready_to_close"
	StageWon           DecisionStage = "won"
	StageLost          DecisionStage = "lost"
	StageDormant       DecisionStage = "dormant"
)

// SignalType represents the type of a trading signal
type SignalType string

const (
	SignalTypeBuying     SignalType = "buying"
	SignalTypeRisk       SignalType = "risk"
	SignalTypeStall      SignalType = "stall"
	SignalTypeCommitment SignalType = "commitment"
)

// ProspectStatus represents the status of a sales prospect
type ProspectStatus string

const (
	ProspectStatusActive  ProspectStatus = "active"
	ProspectStatusWon     ProspectStatus = "won"
	ProspectStatusLost    ProspectStatus = "lost"
	ProspectStatusDormant ProspectStatus = "dormant"
)

// StageChangeType represents how a stage change was initiated
type StageChangeType string

const (
	StageChangeAISuggested StageChangeType = "ai_suggested"
	StageChangeManual      StageChangeType = "manual"
)

// ConversationType represents the type of sales conversation
type ConversationType string

const (
	ConversationFaceToFace ConversationType = "face_to_face"
	ConversationPhone      ConversationType = "phone"
	ConversationVideo      ConversationType = "video"
)

// ConversationPurpose represents the purpose of a sales conversation
type ConversationPurpose string

const (
	PurposeFirstContact  ConversationPurpose = "first_contact"
	PurposeNeedDiscovery ConversationPurpose = "need_discovery"
	PurposeDemo          ConversationPurpose = "demo"
	PurposeObjection     ConversationPurpose = "objection"
	PurposeClosing       ConversationPurpose = "closing"
)

// Lingce Sales Scene Types
const (
	SceneTypeLingceSalesConversation = "lingce_sales_conversation"
	SceneTypeLingceSalesShift        = "lingce_sales_shift"
	SceneTypeLingceSalesMemo         = "lingce_sales_memo"
	RoleCategoryLingceSales          = "lingce_sales"
)

// LingceSalesProspect represents a sales prospect (customer)
type LingceSalesProspect struct {
	ID                 int64          `json:"id"`
	TenantID           int64          `json:"tenant_id"`
	InstitutionName    string         `json:"institution_name"`
	InstitutionType    string         `json:"institution_type"`
	InstitutionScale   string         `json:"institution_scale"`
	Region             string         `json:"region"`
	ContactName        string         `json:"contact_name"`
	ContactRole        string         `json:"contact_role"`
	ContactPhone       string         `json:"contact_phone"`
	ContactWechat      string         `json:"contact_wechat"`
	PainPoints         JSONObject     `json:"pain_points,omitempty"`
	DecisionStage      DecisionStage  `json:"decision_stage"`
	DealProbability    string         `json:"deal_probability"`
	BudgetSignal       string         `json:"budget_signal"`
	CompetitorMentions JSONObject     `json:"competitor_mentions,omitempty"`
	DecisionChain      JSONObject     `json:"decision_chain,omitempty"`
	InternalSupporters string         `json:"internal_supporters"`
	InternalBlockers   string         `json:"internal_blockers"`
	Source             string         `json:"source"`
	AssignedTo         *int64         `json:"assigned_to,omitempty"`
	NextAction         string         `json:"next_action"`
	NextFollowUpAt     *time.Time     `json:"next_follow_up_at,omitempty"`
	Status             ProspectStatus `json:"status"`
	WonAt              *time.Time     `json:"won_at,omitempty"`
	LostReason         string         `json:"lost_reason"`
	Notes              string         `json:"notes"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

// LingceSalesProspectRecording represents the association between a prospect and a recording
type LingceSalesProspectRecording struct {
	ID                  int64               `json:"id"`
	ProspectID          int64               `json:"prospect_id"`
	RecordingID         int64               `json:"recording_id"`
	ConversationType    ConversationType    `json:"conversation_type"`
	ConversationPurpose ConversationPurpose `json:"conversation_purpose"`
	CreatedAt           time.Time           `json:"created_at"`
}

// LingceSalesProspectStageChange represents a stage change history record
type LingceSalesProspectStageChange struct {
	ID          int64           `json:"id"`
	ProspectID  int64           `json:"prospect_id"`
	RecordingID *int64          `json:"recording_id,omitempty"`
	FromStage   DecisionStage   `json:"from_stage"`
	ToStage     DecisionStage   `json:"to_stage"`
	ChangeType  StageChangeType `json:"change_type"`
	Reason      string          `json:"reason"`
	ChangedBy   *int64          `json:"changed_by,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// SuggestedStageChange represents AI's suggestion for a stage change (embedded in analysis_result)
type SuggestedStageChange struct {
	CurrentStage   string   `json:"current_stage"`
	SuggestedStage string   `json:"suggested_stage"`
	Confidence     string   `json:"confidence"`
	Reason         string   `json:"reason"`
	EvidenceQuotes []string `json:"evidence_quotes"`
}

// Signal represents a trading signal extracted from a conversation (embedded in analysis_result)
type Signal struct {
	Type           string `json:"type"`
	Text           string `json:"text"`
	Timestamp      string `json:"timestamp"`
	Confidence     string `json:"confidence"`
	Interpretation string `json:"interpretation"`
}
