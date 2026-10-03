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

type CurrentQuality struct {
	*CheckRun
	CheckedAt time.Time `json:"checked_at"`
}

type currentQualityPackage struct {
	Status            string         `json:"status"`
	OverallResult     string         `json:"overall_result"`
	TriggerAction     string         `json:"trigger_action"`
	SnapshotID        string         `json:"snapshot_id,omitempty"`
	TemplateVersionID string         `json:"template_version_id,omitempty"`
	StartedAt         time.Time      `json:"started_at,omitempty"`
	CompletedAt       *time.Time     `json:"completed_at,omitempty"`
	CheckedAt         time.Time      `json:"checked_at"`
	FailureReason     string         `json:"failure_reason,omitempty"`
	ResultCount       int            `json:"result_count"`
	AttentionCount    int            `json:"attention_count"`
	BlockingCount     int            `json:"blocking_count"`
	Results           []*CheckResult `json:"results"`
}

type CheckResult struct {
	ID                      string         `json:"id"`
	QualityRequirementID    string         `json:"quality_requirement_id"`
	RequirementCode         string         `json:"requirement_code"`
	RequirementName         string         `json:"requirement_name"`
	FieldCode               string         `json:"field_code,omitempty"`
	FieldName               string         `json:"field_name,omitempty"`
	FactCode                string         `json:"fact_code,omitempty"`
	RuleCode                string         `json:"rule_code,omitempty"`
	RuleName                string         `json:"rule_name,omitempty"`
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
	RecordContext     map[string]any `json:"record_context,omitempty"`
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
