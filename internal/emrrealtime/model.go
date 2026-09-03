package emrrealtime

import "time"

type StartRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	CustomerID        *int64 `json:"customer_id,omitempty"`
	TemplateVersionID string `json:"template_version_id,omitempty"`
	DocumentType      string `json:"document_type,omitempty"`
	VisitType         string `json:"visit_type,omitempty"`
	DepartmentID      *int64 `json:"department_id,omitempty"`
}

type BindCustomerRequest struct {
	CustomerID int64 `json:"customer_id"`
}

type SessionResponse struct {
	EncounterID       int64          `json:"encounter_id"`
	RecordID          string         `json:"record_id"`
	TemplateVersionID string         `json:"template_version_id"`
	PatientID         *int64         `json:"patient_id,omitempty"`
	PatientSnapshot   map[string]any `json:"patient_snapshot"`
	StartedAt         time.Time      `json:"started_at"`
}

type FinalizeResponse struct {
	EncounterID int64  `json:"encounter_id"`
	RecordID    string `json:"record_id"`
	RecordingID int64  `json:"recording_id"`
	Queued      bool   `json:"queued"`
}

type modelSelection struct {
	Provider  string
	ModelCode string
}

type patientInfo struct {
	ID        int64
	PatientID *int64
	Name      string
	Phone     *string
	Gender    *string
	Age       *int
}

type realtimeASRResult struct {
	Sequence  int     `json:"sequence"`
	Text      string  `json:"text"`
	Final     bool    `json:"final"`
	StartTime float64 `json:"start_time,omitempty"`
	EndTime   float64 `json:"end_time,omitempty"`
}

type realtimeTranscriptSegment struct {
	Sequence  int     `json:"sequence"`
	Text      string  `json:"text"`
	StartTime float64 `json:"start_time,omitempty"`
	EndTime   float64 `json:"end_time,omitempty"`
}

type realtimePrompt struct {
	SystemPrompt string
	UserPrompt   string
}
