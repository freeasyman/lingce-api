package recording

import (
	"context"
	"fmt"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// ListRecordings retrieves a paginated list of medical recordings
func (s *Service) ListRecordings(ctx context.Context, req RecordingListRequest) ([]*RecordingResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	recordings, total, err := s.store.ListRecordings(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*RecordingResponse, len(recordings))
	for i, r := range recordings {
		responses[i] = toRecordingResponse(r)
	}

	return responses, total, nil
}

// GetRecording retrieves a medical recording by ID
func (s *Service) GetRecording(ctx context.Context, id int64) (*RecordingResponse, error) {
	recording, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toRecordingResponse(recording), nil
}

// CreateRecording creates a new medical recording
func (s *Service) CreateRecording(ctx context.Context, req CreateRecordingRequest) (*RecordingResponse, error) {
	// Validate request
	if req.TenantID == 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if req.EmployeeID == 0 {
		return nil, fmt.Errorf("employee_id is required")
	}
	if req.PatientName == "" {
		return nil, fmt.Errorf("patient_name is required")
	}
	if req.RecordingURL == "" {
		return nil, fmt.Errorf("recording_url is required")
	}

	recording, err := s.store.CreateRecording(ctx, req)
	if err != nil {
		return nil, err
	}

	return toRecordingResponse(recording), nil
}

// UpdateRecording updates a medical recording
func (s *Service) UpdateRecording(ctx context.Context, id int64, req UpdateRecordingRequest) (*RecordingResponse, error) {
	recording, err := s.store.UpdateRecording(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toRecordingResponse(recording), nil
}

// DeleteRecording deletes a medical recording
func (s *Service) DeleteRecording(ctx context.Context, id int64) error {
	return s.store.DeleteRecording(ctx, id)
}

// toRecordingResponse converts a MedicalRecording to RecordingResponse
func toRecordingResponse(r *MedicalRecording) *RecordingResponse {
	resp := &RecordingResponse{
		ID:                r.ID,
		TenantID:          r.TenantID,
		EmployeeID:        r.EmployeeID,
		PatientName:       r.PatientName,
		PatientAge:        r.PatientAge,
		PatientGender:     r.PatientGender,
		PatientPhone:      r.PatientPhone,
		RecordingURL:      r.RecordingURL,
		RecordingDuration: r.RecordingDuration,
		TranscriptText:    r.TranscriptText,
		DoctorSummary:     r.DoctorSummary,
		TherapistSummary:  r.TherapistSummary,
		ConsultantSummary: r.ConsultantSummary,
		Status:            string(r.Status),
		ProcessingError:   r.ProcessingError,
		CreatedAt:         r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:         r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if r.RecordingStartedAt != nil {
		formatted := r.RecordingStartedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.RecordingStartedAt = &formatted
	}

	if r.RecordingEndedAt != nil {
		formatted := r.RecordingEndedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.RecordingEndedAt = &formatted
	}

	if r.ProcessedAt != nil {
		formatted := r.ProcessedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ProcessedAt = &formatted
	}

	return resp
}
