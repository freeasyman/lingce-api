package recording

import "time"

// CreateRecordingRequest represents a request to create a medical recording
type CreateRecordingRequest struct {
	TenantID           int64      `json:"tenant_id"`
	EmployeeID         int64      `json:"employee_id"`
	PatientName        string     `json:"patient_name"`
	PatientAge         *int       `json:"patient_age,omitempty"`
	PatientGender      *string    `json:"patient_gender,omitempty"`
	PatientPhone       *string    `json:"patient_phone,omitempty"`
	RecordingURL       string     `json:"recording_url"`
	RecordingDuration  *int       `json:"recording_duration,omitempty"`
	RecordingStartedAt *time.Time `json:"recording_started_at,omitempty"`
	RecordingEndedAt   *time.Time `json:"recording_ended_at,omitempty"`
}

// UpdateRecordingRequest represents a request to update a medical recording
type UpdateRecordingRequest struct {
	PatientName       *string         `json:"patient_name,omitempty"`
	PatientAge        *int            `json:"patient_age,omitempty"`
	PatientGender     *string         `json:"patient_gender,omitempty"`
	PatientPhone      *string         `json:"patient_phone,omitempty"`
	TranscriptText    *string         `json:"transcript_text,omitempty"`
	DoctorSummary     *string         `json:"doctor_summary,omitempty"`
	TherapistSummary  *string         `json:"therapist_summary,omitempty"`
	ConsultantSummary *string         `json:"consultant_summary,omitempty"`
	Status            *RecordingStatus `json:"status,omitempty"`
	ProcessingError   *string         `json:"processing_error,omitempty"`
	ProcessedAt       *time.Time      `json:"processed_at,omitempty"`
}

// RecordingResponse represents a medical recording response
type RecordingResponse struct {
	ID                 int64      `json:"id"`
	TenantID           int64      `json:"tenant_id"`
	EmployeeID         int64      `json:"employee_id"`
	PatientName        string     `json:"patient_name"`
	PatientAge         *int       `json:"patient_age,omitempty"`
	PatientGender      *string    `json:"patient_gender,omitempty"`
	PatientPhone       *string    `json:"patient_phone,omitempty"`
	RecordingURL       string     `json:"recording_url"`
	RecordingDuration  *int       `json:"recording_duration,omitempty"`
	TranscriptText     *string    `json:"transcript_text,omitempty"`
	DoctorSummary      *string    `json:"doctor_summary,omitempty"`
	TherapistSummary   *string    `json:"therapist_summary,omitempty"`
	ConsultantSummary  *string    `json:"consultant_summary,omitempty"`
	Status             string     `json:"status"`
	ProcessingError    *string    `json:"processing_error,omitempty"`
	RecordingStartedAt *string    `json:"recording_started_at,omitempty"`
	RecordingEndedAt   *string    `json:"recording_ended_at,omitempty"`
	ProcessedAt        *string    `json:"processed_at,omitempty"`
	CreatedAt          string     `json:"created_at"`
	UpdatedAt          string     `json:"updated_at"`
}

// RecordingListRequest represents a request to list medical recordings
type RecordingListRequest struct {
	TenantID    int64            `json:"tenant_id"`
	EmployeeID  *int64           `json:"employee_id,omitempty"`
	PatientName *string          `json:"patient_name,omitempty"`
	Status      *RecordingStatus `json:"status,omitempty"`
	StartDate   *time.Time       `json:"start_date,omitempty"`
	EndDate     *time.Time       `json:"end_date,omitempty"`
	Page        int              `json:"page"`
	PageSize    int              `json:"page_size"`
}
