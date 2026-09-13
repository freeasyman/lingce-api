package complianceguard

import "time"

type AnalyzeTextRequest struct {
	TenantID      int64  `json:"tenant_id"`
	SceneCode     string `json:"scene_code"`
	SourceType    string `json:"source_type"`
	SourceID      string `json:"source_id"`
	SourceVersion string `json:"source_version"`
	Text          string `json:"text"`
	SampleID      string `json:"sample_id,omitempty"`
}

// CommunicationCheckRequest 是 Worker 提交给合规卫士的正式沟通检查请求。
//
// Worker 只提交已经完成的录音素材和客观关联信息；提示词、规则集、
// 模型配置和结果入库均由合规卫士 API 负责。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
type CommunicationCheckRequest struct {
	TenantID          int64  `json:"tenant_id"`
	RecordingID       int64  `json:"recording_id"`
	EncounterID       int64  `json:"encounter_id"`
	EmployeeID        int64  `json:"employee_id"`
	CustomerID        int64  `json:"customer_id"`
	CustomerName      string `json:"customer_name"`
	EmployeeName      string `json:"employee_name"`
	DoctorName        string `json:"doctor_name"`
	BusinessScope     string `json:"business_scope"`
	RoleCode          string `json:"role_code"`
	RecordedAt        string `json:"recorded_at"`
	CleanedTranscript string `json:"cleaned_transcript"`
}

// CommunicationCheckResponse 是 API 对 Worker 的接收确认。
//
// accepted 只表示请求已通过同步校验并持久化，不表示模型已经完成。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
type CommunicationCheckResponse struct {
	Code           string `json:"code"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	RequestID      string `json:"request_id"`
	RunID          string `json:"run_id"`
	TenantID       int64  `json:"tenant_id"`
	RecordingID    int64  `json:"recording_id"`
	EncounterID    int64  `json:"encounter_id"`
	EmployeeID     int64  `json:"employee_id"`
	CustomerID     int64  `json:"customer_id"`
	RuleSetCode    string `json:"rule_set_code"`
	RuleSetVersion string `json:"rule_set_version"`
	PromptCode     string `json:"prompt_code"`
	PromptVersion  string `json:"prompt_version"`
	ModelConfigID  int64  `json:"model_config_id"`
	ModelCode      string `json:"model_code"`
	RawResponse    string `json:"raw_response"`
	Findings       []any  `json:"findings"`
}

type AnalyzeTextResponse struct {
	RunID          string    `json:"run_id"`
	Status         string    `json:"status"`
	PromptCode     string    `json:"prompt_code,omitempty"`
	PromptVersion  string    `json:"prompt_version,omitempty"`
	ModelCode      string    `json:"model_code,omitempty"`
	RuleSetCode    string    `json:"rule_set_code,omitempty"`
	RuleSetVersion string    `json:"rule_set_version,omitempty"`
	RawResponse    string    `json:"raw_response,omitempty"`
	Findings       []any     `json:"findings,omitempty"`
	Error          string    `json:"error,omitempty"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
}
