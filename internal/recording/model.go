package recording

import "time"

// RecordingStatus represents the status of a medical recording
type RecordingStatus string

const (
	StatusPending    RecordingStatus = "pending"
	StatusProcessing RecordingStatus = "processing"
	StatusCompleted  RecordingStatus = "completed"
	StatusFailed     RecordingStatus = "failed"
)

// MedicalRecording represents a medical consultation recording
type MedicalRecording struct {
	ID                 int64           `json:"id"`
	TenantID           int64           `json:"tenant_id"`
	EmployeeID         int64           `json:"employee_id"`
	PatientName        string          `json:"patient_name"`
	PatientAge         *int            `json:"patient_age,omitempty"`
	PatientGender      *string         `json:"patient_gender,omitempty"`
	PatientPhone       *string         `json:"patient_phone,omitempty"`
	RecordingURL       string          `json:"recording_url"`
	RecordingDuration  *int            `json:"recording_duration,omitempty"`
	TranscriptText     *string         `json:"transcript_text,omitempty"`
	DoctorSummary      *string         `json:"doctor_summary,omitempty"`
	TherapistSummary   *string         `json:"therapist_summary,omitempty"`
	ConsultantSummary  *string         `json:"consultant_summary,omitempty"`
	Status             RecordingStatus `json:"status"`
	ProcessingError    *string         `json:"processing_error,omitempty"`
	RecordingStartedAt *time.Time      `json:"recording_started_at,omitempty"`
	RecordingEndedAt   *time.Time      `json:"recording_ended_at,omitempty"`
	ProcessedAt        *time.Time      `json:"processed_at,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	DeletedAt          *time.Time      `json:"deleted_at,omitempty"`
}
