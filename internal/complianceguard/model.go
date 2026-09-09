package complianceguard

import "time"

type AnalyzeTextRequest struct {
	TenantID    int64  `json:"tenant_id"`
	SceneCode   string `json:"scene_code"`
	SourceType  string `json:"source_type"`
	SourceID    string `json:"source_id"`
	SourceVersion string `json:"source_version"`
	Text        string `json:"text"`
	SampleID    string `json:"sample_id,omitempty"`
}

type AnalyzeTextResponse struct {
	RunID          string         `json:"run_id"`
	Status         string         `json:"status"`
	PromptCode     string         `json:"prompt_code,omitempty"`
	PromptVersion  string         `json:"prompt_version,omitempty"`
	ModelCode      string         `json:"model_code,omitempty"`
	RuleSetCode    string         `json:"rule_set_code,omitempty"`
	RuleSetVersion string         `json:"rule_set_version,omitempty"`
	RawResponse    string         `json:"raw_response,omitempty"`
	Findings       []any          `json:"findings,omitempty"`
	Error          string         `json:"error,omitempty"`
	FinishedAt     time.Time      `json:"finished_at,omitempty"`
}
