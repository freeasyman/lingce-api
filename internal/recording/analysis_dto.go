package recording

import "time"

type AnalysisRouteListRequest struct {
	TenantIDs []int64
	TenantID  *int64
	RoleID    *int64
	SceneCode string
	Keyword   string
	Enabled   *bool
	Page      int
	PageSize  int
}

type CreateAnalysisRouteRequest struct {
	TenantID        int64      `json:"tenant_id"`
	RoleID          int64      `json:"role_id"`
	SceneCode       string     `json:"scene_code"`
	PipelineCode    string     `json:"pipeline_code"`
	PipelineVersion string     `json:"pipeline_version"`
	IsEnabled       *bool      `json:"is_enabled,omitempty"`
	EffectiveAt     *time.Time `json:"effective_at,omitempty"`
	Notes           *string    `json:"notes,omitempty"`
}

type UpdateAnalysisRouteRequest struct {
	RoleID          *int64     `json:"role_id,omitempty"`
	SceneCode       *string    `json:"scene_code,omitempty"`
	PipelineCode    *string    `json:"pipeline_code,omitempty"`
	PipelineVersion *string    `json:"pipeline_version,omitempty"`
	IsEnabled       *bool      `json:"is_enabled,omitempty"`
	EffectiveAt     *time.Time `json:"effective_at,omitempty"`
	Notes           *string    `json:"notes,omitempty"`
}

type PublishAnalysisRouteRequest struct {
	EffectiveAt *time.Time `json:"effective_at,omitempty"`
}

type AnalysisRunListRequest struct {
	TenantIDs     []int64
	TenantID      *int64
	Status        string
	RoleCode      string
	PipelineCode  string
	TriggerSource string
	RecordingID   *int64
	TraceID       string
	Keyword       string
	Page          int
	PageSize      int
}
