package dealexpert

import "time"

type ReceiveRequest struct {
	TenantID                int64  `json:"tenant_id"`
	RecordingID             int64  `json:"recording_id"`
	EncounterID             int64  `json:"encounter_id"`
	EmployeeID              int64  `json:"employee_id"`
	CustomerID              int64  `json:"customer_id"`
	CustomerName            string `json:"customer_name"`
	BusinessScope           string `json:"business_scope"`
	RoleCode                string `json:"role_code"`
	RecordedAt              string `json:"recorded_at"`
	CleanedTranscript       string `json:"cleaned_transcript"`
	AnalysisPipelineVersion string `json:"analysis_pipeline_version"`
	PromptVersion           string `json:"prompt_version"`
}

type Analysis struct {
	ID                      int64
	TenantID                int64
	RecordingID             int64
	EncounterID             int64
	EmployeeID              int64
	CustomerID              *int64
	CustomerName            string
	BusinessScope           string
	RoleCode                string
	RecordedAt              *time.Time
	AnalysisPipelineVersion string
	PromptVersion           string
	Status                  string
	ReceivedAt              time.Time
	UpdatedAt               time.Time
}
