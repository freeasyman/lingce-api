package emrrealtime

import (
	"time"

	"github.com/google/uuid"
)

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

type StartBadgeSessionRequest struct {
	DeviceNo   string `json:"device_no"`
	CustomerID *int64 `json:"customer_id,omitempty"`
}

type StopBadgeSessionRequest struct{}

type BadgeSessionResponse struct {
	ID          uuid.UUID  `json:"id"`
	OrderNo     string     `json:"order_no"`
	Status      string     `json:"status"`
	DeviceNo    string     `json:"device_no"`
	CustomerID  *int64     `json:"customer_id,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	StoppedAt   *time.Time `json:"stopped_at,omitempty"`
	RecordingID int64      `json:"recording_id,omitempty"`
	EncounterID int64      `json:"encounter_id,omitempty"`
	RecordID    string     `json:"record_id,omitempty"`
	Message     string     `json:"message,omitempty"`
}

type FinalizeBadgeRequest struct {
	SessionID   string `json:"session_id,omitempty"`
	CustomerID  int64  `json:"customer_id,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	RecordingID int64  `json:"recording_id,omitempty"`
	EncounterID int64  `json:"encounter_id,omitempty"`
}

type FinalizeBadgeResponse struct {
	Status      string `json:"status"`
	RecordingID int64  `json:"recording_id,omitempty"`
	EncounterID int64  `json:"encounter_id,omitempty"`
	RecordID    string `json:"record_id,omitempty"`
	Message     string `json:"message,omitempty"`
}

type SessionResponse struct {
	EncounterID       int64          `json:"encounter_id"`
	RecordID          string         `json:"record_id"`
	TemplateVersionID string         `json:"template_version_id"`
	PatientID         *int64         `json:"patient_id,omitempty"`
	PatientSnapshot   map[string]any `json:"patient_snapshot"`
	StartedAt         time.Time      `json:"started_at"`
}

// IncrementalDraftRequest is intentionally scoped to the experimental
// consultation page. It asks the model for field-level additions based on only
// the new transcript delta; it never replaces the complete working draft.
type IncrementalDraftRequest struct {
	EncounterID     int64          `json:"encounter_id"`
	RecordID        string         `json:"record_id"`
	TranscriptDelta string         `json:"transcript_delta"`
	CurrentContent  map[string]any `json:"current_content"`
	LockedFields    []string       `json:"locked_fields,omitempty"`
}

type IncrementalDraftResponse struct {
	Updates         map[string]any `json:"updates"`
	UnresolvedItems []any          `json:"unresolved_items,omitempty"`
}

type FinalizeResponse struct {
	EncounterID int64  `json:"encounter_id"`
	RecordID    string `json:"record_id"`
	RecordingID int64  `json:"recording_id"`
	Queued      bool   `json:"queued"`
	QueueError  string `json:"queue_error,omitempty"`
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
	ExtraData map[string]any
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

type realtimeTranscriptCorrection struct {
	Sequence      int     `json:"sequence"`
	OriginalText  string  `json:"original_text"`
	CorrectedText string  `json:"corrected_text"`
	StartTime     float64 `json:"start_time,omitempty"`
	EndTime       float64 `json:"end_time,omitempty"`
}

type realtimePrompt struct {
	SystemPrompt string
	UserPrompt   string
}
