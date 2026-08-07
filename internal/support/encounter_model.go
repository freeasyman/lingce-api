package support

import "time"

type Encounter struct {
	ID                  int64      `json:"id"`
	TenantID            int64      `json:"tenant_id"`
	RecordingID         int64      `json:"recording_id"`
	AnalysisRunID       *int64     `json:"analysis_run_id,omitempty"`
	SequenceNo          int        `json:"sequence_no"`
	Title               string     `json:"title"`
	Summary             *string    `json:"summary,omitempty"`
	PatientName         *string    `json:"patient_name,omitempty"`
	PatientAge          *int       `json:"patient_age,omitempty"`
	PatientGender       *string    `json:"patient_gender,omitempty"`
	PatientPhone        *string    `json:"patient_phone,omitempty"`
	EmployeeID          *int64     `json:"employee_id,omitempty"`
	EmployeeName        *string    `json:"employee_name,omitempty"`
	Scene               *string    `json:"scene,omitempty"`
	StartSeconds        *int       `json:"start_seconds,omitempty"`
	EndSeconds          *int       `json:"end_seconds,omitempty"`
	StartAt             *time.Time `json:"start_at,omitempty"`
	EndAt               *time.Time `json:"end_at,omitempty"`
	SourcePromptCode    *string    `json:"source_prompt_code,omitempty"`
	SourcePromptVersion *string    `json:"source_prompt_version,omitempty"`
	SourceType          string     `json:"source_type"`
	AnalysisPayload     JSONObject `json:"analysis_payload,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}
