package recording

import "time"

type AnalysisRouteStatus string

type AnalysisRunStatus string

type AnalysisStepStatus string

const (
	AnalysisRouteStatusDraft     AnalysisRouteStatus = "draft"
	AnalysisRouteStatusPublished AnalysisRouteStatus = "published"
	AnalysisRouteStatusDisabled  AnalysisRouteStatus = "disabled"
)

const (
	AnalysisRunStatusQueued    AnalysisRunStatus = "queued"
	AnalysisRunStatusRunning   AnalysisRunStatus = "running"
	AnalysisRunStatusSucceeded AnalysisRunStatus = "succeeded"
	AnalysisRunStatusFailed    AnalysisRunStatus = "failed"
	AnalysisRunStatusCancelled AnalysisRunStatus = "cancelled"
)

const (
	AnalysisStepStatusPending   AnalysisStepStatus = "pending"
	AnalysisStepStatusRunning   AnalysisStepStatus = "running"
	AnalysisStepStatusSucceeded AnalysisStepStatus = "succeeded"
	AnalysisStepStatusFailed    AnalysisStepStatus = "failed"
	AnalysisStepStatusSkipped   AnalysisStepStatus = "skipped"
)

type AnalysisRoleOption struct {
	RoleID     int64  `json:"role_id"`
	RoleCode   string `json:"role_code"`
	RoleName   string `json:"role_name"`
	TenantID   int64  `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	Source     string `json:"source"`
}

type AnalysisPipelineOption struct {
	PipelineCode  string  `json:"pipeline_code"`
	PipelineName  string  `json:"pipeline_name"`
	Version       string  `json:"version"`
	Description   *string `json:"description,omitempty"`
	ReleaseStatus string  `json:"release_status"`
	ReleaseNote   *string `json:"release_note,omitempty"`
}

type AnalysisRouteRecord struct {
	ID                  int64               `json:"id"`
	TenantID            int64               `json:"tenant_id"`
	TenantName          string              `json:"tenant_name"`
	RoleID              int64               `json:"role_id"`
	RoleCode            string              `json:"role_code"`
	RoleName            string              `json:"role_name"`
	SceneCode           string              `json:"scene_code"`
	SceneName           string              `json:"scene_name"`
	PipelineCode        string              `json:"pipeline_code"`
	PipelineName        string              `json:"pipeline_name"`
	PipelineVersion     string              `json:"pipeline_version"`
	Status              AnalysisRouteStatus `json:"status"`
	IsEnabled           bool                `json:"is_enabled"`
	EffectiveAt         time.Time           `json:"effective_at"`
	UpdatedAt           time.Time           `json:"updated_at"`
	UpdatedBy           string              `json:"updated_by"`
	PublishedAt         *time.Time          `json:"published_at,omitempty"`
	PublishedBy         *string             `json:"published_by,omitempty"`
	Notes               *string             `json:"notes,omitempty"`
	PipelineDescription *string             `json:"pipeline_description,omitempty"`
	PipelineReleaseNote *string             `json:"pipeline_release_note,omitempty"`
}

type AnalysisRunSnapshot struct {
	RoleSourceType      string     `json:"role_source_type"`
	RoleID              int64      `json:"role_id"`
	RoleCode            string     `json:"role_code"`
	RoleName            string     `json:"role_name"`
	PipelineCode        string     `json:"pipeline_code"`
	PipelineVersion     string     `json:"pipeline_version"`
	RouteConfigID       int64      `json:"route_config_id"`
	RouteUpdatedAt      *time.Time `json:"route_updated_at,omitempty"`
	PipelineReleaseNote *string    `json:"pipeline_release_note,omitempty"`
}

type AnalysisRunRecord struct {
	ID                int64                `json:"id"`
	RecordingID       int64                `json:"recording_id"`
	VendorRecordingID *string              `json:"vendor_recording_id,omitempty"`
	TenantID          int64                `json:"tenant_id"`
	TenantName        string               `json:"tenant_name"`
	TraceID           *string              `json:"trace_id,omitempty"`
	ResolvedRoleID    *int64               `json:"resolved_role_id,omitempty"`
	ResolvedRoleCode  *string              `json:"resolved_role_code,omitempty"`
	ResolvedRoleName  *string              `json:"resolved_role_name,omitempty"`
	SceneCode         *string              `json:"scene_code,omitempty"`
	SceneName         *string              `json:"scene_name,omitempty"`
	PipelineCode      string               `json:"pipeline_code"`
	PipelineName      *string              `json:"pipeline_name,omitempty"`
	PipelineVersion   string               `json:"pipeline_version"`
	Status            AnalysisRunStatus    `json:"status"`
	TriggerSource     string               `json:"trigger_source"`
	RouteConfigID     *int64               `json:"route_config_id,omitempty"`
	SnapshotVersion   *string              `json:"snapshot_version,omitempty"`
	StartedAt         *time.Time           `json:"started_at,omitempty"`
	EndedAt           *time.Time           `json:"ended_at,omitempty"`
	DurationMs        *int64               `json:"duration_ms,omitempty"`
	ErrorCode         *string              `json:"error_code,omitempty"`
	ErrorMessage      *string              `json:"error_message,omitempty"`
	Snapshot          *AnalysisRunSnapshot `json:"snapshot,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
}

type AnalysisStepRecord struct {
	ID              int64              `json:"id"`
	RunID           int64              `json:"run_id"`
	StepCode        string             `json:"step_code"`
	StepName        string             `json:"step_name"`
	StepType        string             `json:"step_type"`
	PromptCode      *string            `json:"prompt_code,omitempty"`
	PromptVersion   *string            `json:"prompt_version,omitempty"`
	Status          AnalysisStepStatus `json:"status"`
	Attempt         int                `json:"attempt"`
	ExecutionTimeMs *int               `json:"execution_time_ms,omitempty"`
	StartedAt       *time.Time         `json:"started_at,omitempty"`
	EndedAt         *time.Time         `json:"ended_at,omitempty"`
	ErrorCode       *string            `json:"error_code,omitempty"`
	ErrorMessage    *string            `json:"error_message,omitempty"`
	InputSummary    *string            `json:"input_summary,omitempty"`
	OutputSummary   *string            `json:"output_summary,omitempty"`
	TokensUsed      *int               `json:"tokens_used,omitempty"`
	CostAmount      *float64           `json:"cost_amount,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
}
