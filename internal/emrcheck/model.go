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
	Conclusion              string         `json:"conclusion"`
	CheckStatus             string         `json:"check_status"`
	IncompleteReason        string         `json:"incomplete_reason,omitempty"`
	HandlingResult          string         `json:"handling_result"`
	MeetsDeadline           *bool          `json:"meets_deadline,omitempty"`
	Evidence                map[string]any `json:"evidence,omitempty"`
	HitExplanation          string         `json:"hit_explanation,omitempty"`
	SuggestedHandling       string         `json:"suggested_handling,omitempty"`
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
