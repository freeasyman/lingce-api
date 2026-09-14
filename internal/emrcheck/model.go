package emrcheck

import "time"

type CheckRun struct {
	ID                string         `json:"id"`
	TenantID          int64          `json:"tenant_id"`
	RecordID          string         `json:"record_id"`
	SnapshotID        string         `json:"snapshot_id"`
	TemplateVersionID string         `json:"template_version_id"`
	TriggerAction     string         `json:"trigger_action"`
	Status            string         `json:"status"`
	OverallResult     string         `json:"overall_result"`
	StartedAt         time.Time      `json:"started_at"`
	CompletedAt       *time.Time     `json:"completed_at,omitempty"`
	StartedBy         *int64         `json:"started_by,omitempty"`
	FailureReason     string         `json:"failure_reason,omitempty"`
	Results           []*CheckResult `json:"results,omitempty"`
}

type CheckResult struct {
	ID                      string         `json:"id"`
	CheckRunID              string         `json:"check_run_id"`
	QualityRequirementID    string         `json:"quality_requirement_id"`
	RequirementCode         string         `json:"requirement_code"`
	RequirementName         string         `json:"requirement_name"`
	ConfiguredExecutionMode string         `json:"configured_execution_mode"`
	ActualExecutionMode     string         `json:"actual_execution_mode"`
	ExecutionStatus         string         `json:"execution_status"`
	Conclusion              string         `json:"conclusion"`
	CheckStatus             string         `json:"check_status"`
	IncompleteReason        string         `json:"incomplete_reason,omitempty"`
	HandlingResult          string         `json:"handling_result"`
	MeetsDeadline           *bool          `json:"meets_deadline,omitempty"`
	Evidence                map[string]any `json:"evidence,omitempty"`
	HitExplanation          string         `json:"hit_explanation,omitempty"`
	SuggestedHandling       string         `json:"suggested_handling,omitempty"`
	Severity                string         `json:"severity,omitempty"`
	MissingFacts            []string       `json:"missing_facts,omitempty"`
	Confidence              string         `json:"confidence,omitempty"`
	ManualConclusion        *string        `json:"manual_conclusion,omitempty"`
	ManualBy                *int64         `json:"manual_by,omitempty"`
	ManualAt                *time.Time     `json:"manual_at,omitempty"`
}

type CheckRequest struct {
	TenantID      int64
	RecordID      string
	SnapshotID    string
	TriggerAction string
	StartedBy     *int64
}

type CheckPreviewRequest struct {
	TenantID          int64          `json:"tenant_id"`
	TemplateVersionID string         `json:"template_version_id"`
	VisitType         string         `json:"visit_type"`
	Specialty         string         `json:"specialty,omitempty"`
	ConfirmedBy       *int64         `json:"confirmed_by,omitempty"`
	Content           map[string]any `json:"content"`
	TriggerAction     string         `json:"trigger_action,omitempty"`
	PatientContext    map[string]any `json:"patient_context,omitempty"`
	EncounterContext  map[string]any `json:"encounter_context,omitempty"`
}

type CompletePreviewResponse struct {
	Code                 string         `json:"code"`
	TenantID             int64          `json:"tenant_id"`
	TemplateVersionID    string         `json:"template_version_id"`
	TemplateCode         string         `json:"template_code"`
	TemplateVersion      string         `json:"template_version"`
	PromptCode           string         `json:"prompt_code"`
	PromptVersion        string         `json:"prompt_version"`
	ModelProvider        string         `json:"model_provider"`
	ModelCode            string         `json:"model_code"`
	OverallResult        string         `json:"overall_result"`
	ModelCheckSummary    string         `json:"model_check_summary"`
	ProgramResultCount   int            `json:"program_result_count"`
	ModelResultCount     int            `json:"model_result_count"`
	ResultCount          int            `json:"result_count"`
	ExecutionStartedAt   string         `json:"execution_started_at"`
	ExecutionCompletedAt string         `json:"execution_completed_at"`
	ExecutionDurationMS  int64          `json:"execution_duration_ms"`
	ExecutionLogFile     string         `json:"execution_log_file"`
	ResultLogFile        string         `json:"result_log_file"`
	PromptLogFile        string         `json:"prompt_log_file"`
	Results              []*CheckResult `json:"results"`
}
