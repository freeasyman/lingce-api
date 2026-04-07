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
// Recording Statistics Services

// GetStatsOverview retrieves overview statistics
func (s *Service) GetStatsOverview(ctx context.Context, tenantID int64, startDate, endDate *string) (*RecordingStatsOverviewResponse, error) {
	return s.store.GetStatsOverview(ctx, tenantID, startDate, endDate)
}

// GetStatsByScene retrieves statistics by scene
func (s *Service) GetStatsByScene(ctx context.Context, tenantID int64) ([]RecordingStatsBySceneResponse, error) {
	return s.store.GetStatsByScene(ctx, tenantID)
}

// GetStatsBySource retrieves statistics by source
func (s *Service) GetStatsBySource(ctx context.Context, tenantID int64) ([]RecordingStatsBySourceResponse, error) {
	return s.store.GetStatsBySource(ctx, tenantID)
}

// Recording Task Services

// ListRecordingTasks retrieves a paginated list of recording tasks
func (s *Service) ListRecordingTasks(ctx context.Context, req TaskListRequest) ([]*TaskResponse, int, error) {
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
	
	tasks, total, err := s.store.ListRecordingTasks(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	
	responses := make([]*TaskResponse, len(tasks))
	for i, t := range tasks {
		responses[i] = toTaskResponse(t)
	}
	
	return responses, total, nil
}

// GetTask retrieves a recording task by ID
func (s *Service) GetTask(ctx context.Context, id int64) (*TaskResponse, error) {
	task, err := s.store.GetTaskByID(ctx, id)
	if err != nil {
		return nil, err
	}
	
	return toTaskResponse(task), nil
}

// CompleteTask marks a task as completed
func (s *Service) CompleteTask(ctx context.Context, id int64, completedBy int64, req CompleteTaskRequest) error {
	return s.store.CompleteTask(ctx, id, completedBy)
}

// CancelTask marks a task as cancelled
func (s *Service) CancelTask(ctx context.Context, id int64, req CancelTaskRequest) error {
	if req.Reason == "" {
		return fmt.Errorf("cancel reason is required")
	}
	return s.store.CancelTask(ctx, id, req.Reason)
}

// Recording Prompt Services

// ListRecordingPrompts retrieves a paginated list of recording prompts
func (s *Service) ListRecordingPrompts(ctx context.Context, req RecordingPromptListRequest) ([]*RecordingPrompt, int, error) {
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
	
	return s.store.ListRecordingPrompts(ctx, req)
}

// GetRecordingPrompt retrieves a recording prompt by code
func (s *Service) GetRecordingPrompt(ctx context.Context, code string) (*RecordingPrompt, error) {
	return s.store.GetRecordingPromptByCode(ctx, code)
}

// CreateRecordingPrompt creates a new recording prompt
func (s *Service) CreateRecordingPrompt(ctx context.Context, req CreateRecordingPromptRequest) (*RecordingPrompt, error) {
	// Validate request
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.PromptText == "" {
		return nil, fmt.Errorf("prompt_text is required")
	}
	
	return s.store.CreateRecordingPrompt(ctx, req)
}

// UpdateRecordingPrompt updates a recording prompt
func (s *Service) UpdateRecordingPrompt(ctx context.Context, code string, req UpdateRecordingPromptRequest) (*RecordingPrompt, error) {
	return s.store.UpdateRecordingPrompt(ctx, code, req)
}

// DeleteRecordingPrompt deletes a recording prompt
func (s *Service) DeleteRecordingPrompt(ctx context.Context, code string) error {
	return s.store.DeleteRecordingPrompt(ctx, code)
}

// Best Practice Services

// ListBestPractices retrieves a list of best practices
func (s *Service) ListBestPractices(ctx context.Context, tenantID int64) ([]*RecordingBestPractice, error) {
	return s.store.ListBestPractices(ctx, tenantID)
}

// AddBestPractice adds a recording to best practices
func (s *Service) AddBestPractice(ctx context.Context, tenantID, recordingID, createdBy int64, req AddBestPracticeRequest) (*RecordingBestPractice, error) {
	// Validate request
	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	
	return s.store.AddBestPractice(ctx, tenantID, recordingID, createdBy, req)
}

// DeleteBestPractice removes a recording from best practices
func (s *Service) DeleteBestPractice(ctx context.Context, recordingID int64) error {
	return s.store.DeleteBestPractice(ctx, recordingID)
}

// Placeholder methods for remaining endpoints (to be implemented)

// TriggerTranscribe triggers transcription for a recording
func (s *Service) TriggerTranscribe(ctx context.Context, id int64, req TriggerTranscribeRequest) error {
	// TODO: Call recording-worker service to trigger transcription
	return fmt.Errorf("not implemented yet")
}

// TriggerAnalyze triggers analysis for a recording
func (s *Service) TriggerAnalyze(ctx context.Context, id int64, req TriggerAnalyzeRequest) error {
	// TODO: Call recording-worker service to trigger analysis
	return fmt.Errorf("not implemented yet")
}

// GetAnalysisResult retrieves analysis result for a recording
func (s *Service) GetAnalysisResult(ctx context.Context, id int64) (*RecordingAnalysisResult, error) {
	// TODO: Implement analysis result retrieval
	return nil, fmt.Errorf("not implemented yet")
}

// GetQualityControlDashboard retrieves quality control dashboard
func (s *Service) GetQualityControlDashboard(ctx context.Context, tenantID int64) (*QualityControlDashboardResponse, error) {
	// TODO: Implement quality control dashboard
	return nil, fmt.Errorf("not implemented yet")
}

// GetDoctorAbilityRanking retrieves doctor ability ranking
func (s *Service) GetDoctorAbilityRanking(ctx context.Context, tenantID int64) ([]DoctorAbilityRankingResponse, error) {
	// TODO: Implement doctor ability ranking
	return nil, fmt.Errorf("not implemented yet")
}

// GetDoctorAbilityDetail retrieves detailed doctor ability
func (s *Service) GetDoctorAbilityDetail(ctx context.Context, tenantID, employeeID int64) (*DoctorAbilityDetailResponse, error) {
	// TODO: Implement doctor ability detail
	return nil, fmt.Errorf("not implemented yet")
}

// GetCommunicationAnalysis retrieves communication analysis
func (s *Service) GetCommunicationAnalysis(ctx context.Context, tenantID int64) (*CommunicationAnalysisResponse, error) {
	// TODO: Implement communication analysis
	return nil, fmt.Errorf("not implemented yet")
}

// GetWeeklyMeetingMaterial retrieves weekly meeting material
func (s *Service) GetWeeklyMeetingMaterial(ctx context.Context, tenantID int64) (*WeeklyMeetingMaterialResponse, error) {
	// TODO: Implement weekly meeting material
	return nil, fmt.Errorf("not implemented yet")
}

// GetWeeklySummary retrieves weekly summary
func (s *Service) GetWeeklySummary(ctx context.Context, tenantID int64) (*WeeklySummaryResponse, error) {
	// TODO: Implement weekly summary
	return nil, fmt.Errorf("not implemented yet")
}

// GetTeamTrends retrieves team trends
func (s *Service) GetTeamTrends(ctx context.Context, tenantID int64, period string) (*TeamTrendsResponse, error) {
	// TODO: Implement team trends
	return nil, fmt.Errorf("not implemented yet")
}

// GetDailyReport retrieves daily report
func (s *Service) GetDailyReport(ctx context.Context, tenantID int64, date string) (*DailyReportResponse, error) {
	// TODO: Implement daily report
	return nil, fmt.Errorf("not implemented yet")
}

// GetDiagnosis retrieves diagnosis
func (s *Service) GetDiagnosis(ctx context.Context, tenantID int64) (*DiagnosisResponse, error) {
	// TODO: Implement diagnosis
	return nil, fmt.Errorf("not implemented yet")
}

// Helper functions

// toTaskResponse converts a RecordingTask to TaskResponse
func toTaskResponse(t *RecordingTask) *TaskResponse {
	resp := &TaskResponse{
		ID:           t.ID,
		TenantID:     t.TenantID,
		RecordingID:  t.RecordingID,
		TaskType:     string(t.TaskType),
		Title:        t.Title,
		Description:  t.Description,
		AssignedTo:   t.AssignedTo,
		AssignedBy:   t.AssignedBy,
		Status:       string(t.Status),
		CompletedBy:  t.CompletedBy,
		CancelReason: t.CancelReason,
		CreatedAt:    t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	
	if t.DueDate != nil {
		formatted := t.DueDate.Format("2006-01-02T15:04:05Z07:00")
		resp.DueDate = &formatted
	}
	
	if t.CompletedAt != nil {
		formatted := t.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CompletedAt = &formatted
	}
	
	if t.CancelledAt != nil {
		formatted := t.CancelledAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CancelledAt = &formatted
	}
	
	return resp
}
