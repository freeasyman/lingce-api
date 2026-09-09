package followup

// Followup Worker API 契约数据结构。
// 署名：Codex
// 时间：2026-09-09
//
// 说明：
// 1. 这个模块只做 Worker 到随访中心的最小接收与确认。
// 2. 当前版本不生成随访任务结果，只接收清洗后的转写与客观标识。

// GenerateRequest 是 Worker 调用随访中心时提交的最小请求体。
type GenerateRequest struct {
	TenantID          int64  `json:"tenant_id"`
	RecordingID       int64  `json:"recording_id"`
	EncounterID       int64  `json:"encounter_id"`
	EmployeeID        int64  `json:"employee_id"`
	DoctorName        string `json:"doctor_name"`
	CustomerID        int64  `json:"customer_id"`
	CustomerName      string `json:"customer_name"`
	RecordedAt        string `json:"recorded_at"`
	CleanedTranscript string `json:"cleaned_transcript"`
}

// GenerateResponse 是随访中心对 Worker 的最小确认返回。
type GenerateResponse struct {
	Code         string `json:"code"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	RequestID    string `json:"request_id"`
	TenantID     int64  `json:"tenant_id"`
	RecordingID  int64  `json:"recording_id"`
	EncounterID  int64  `json:"encounter_id"`
	EmployeeID   int64  `json:"employee_id"`
	CustomerID   int64  `json:"customer_id"`
	CustomerName string `json:"customer_name"`
	LLMStatus    string `json:"llm_status,omitempty"`
	LLMRequestID string `json:"llm_request_id,omitempty"`
}
