package emrrule

import "time"

type JSONMap map[string]any

type RunRequest struct {
	Stage         string `json:"stage"`
	TriggerSource string `json:"trigger_source,omitempty"`
}

type DoctorActionRequest struct {
	ActionType string `json:"action_type"`
	Comment    string `json:"comment,omitempty"`
}

type RecordInput struct {
	ID               int64
	TenantID         int64
	EncounterID      *int64
	CustomerID       *int64
	TemplateCode     string
	TemplateName     string
	Status           string
	ChiefComplaint   string
	DiagnosisSummary string
	PatientName      string
	PatientGender    string
	PatientAgeText   string
	PatientPhone     string
	EncounteredAt    *time.Time
	ArchivedAt       *time.Time
	UpdatedAt        time.Time
	PatientSnapshot  JSONMap
	DocumentJSON     JSONMap
	LatestVersionNo  *int
}

type RuleDefinition struct {
	ID              string
	Code            string
	Name            string
	Category        string
	Severity        string
	TriggerType     string
	Conditions      JSONMap
	Description     string
	LegalBasis      string
	SuggestedScript string
}

type RuleRun struct {
	ID            int64     `json:"id"`
	RecordID      int64     `json:"record_id"`
	Stage         string    `json:"stage"`
	TriggerSource string    `json:"trigger_source"`
	Status        string    `json:"status"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
}

type RuleHit struct {
	ID            int64          `json:"id"`
	RunID         int64          `json:"run_id"`
	RecordID      int64          `json:"record_id"`
	RuleID        string         `json:"rule_id"`
	RuleCode      string         `json:"rule_code"`
	RuleName      string         `json:"rule_name"`
	Stage         string         `json:"stage"`
	ExecutorType  string         `json:"executor_type"`
	TargetType    string         `json:"target_type"`
	TargetPath    string         `json:"target_path"`
	HitStatus     string         `json:"hit_status"`
	Severity      string         `json:"severity"`
	ActionPolicy  JSONMap        `json:"action_policy"`
	DoctorMessage string         `json:"doctor_message"`
	QCMessage     string         `json:"qc_message"`
	Summary       string         `json:"summary"`
	CurrentState  string         `json:"current_state"`
	IsActive      bool           `json:"is_active"`
	Evidence      []RuleEvidence `json:"evidence,omitempty"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

type RuleEvidence struct {
	ID              int64   `json:"id"`
	HitID           int64   `json:"hit_id"`
	EvidenceType    string  `json:"evidence_type"`
	FieldPath       string  `json:"field_path"`
	FieldLabel      string  `json:"field_label"`
	TextExcerpt     string  `json:"text_excerpt"`
	StructuredValue JSONMap `json:"structured_value"`
	ExpectedValue   JSONMap `json:"expected_value"`
	ActualValue     JSONMap `json:"actual_value"`
}

type RuleSummary struct {
	RecordID          int64  `json:"record_id"`
	RecordVersion     *int   `json:"record_version,omitempty"`
	BlockingCount     int    `json:"blocking_count"`
	ImportantCount    int    `json:"important_count"`
	NoticeCount       int    `json:"notice_count"`
	UnhandledCount    int    `json:"unhandled_count"`
	AcknowledgedCount int    `json:"acknowledged_count"`
	ResolvedCount     int    `json:"resolved_count"`
	CanSubmit         bool   `json:"can_submit"`
	CanArchive        bool   `json:"can_archive"`
	LastRunID         *int64 `json:"last_run_id,omitempty"`
	LastRunStage      string `json:"last_run_stage"`
	LastCheckedAt     string `json:"last_checked_at,omitempty"`
}

type RunResult struct {
	Run     RuleRun     `json:"run"`
	Hits    []RuleHit   `json:"hits"`
	Summary RuleSummary `json:"summary"`
}
