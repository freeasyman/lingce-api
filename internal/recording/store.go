package recording

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ListRecordings retrieves a paginated list of medical recordings
func (s *Store) ListRecordings(ctx context.Context, req RecordingListRequest) ([]*MedicalRecording, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Build WHERE clause
	conditions = append(conditions, "deleted_at IS NULL")

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
	args = append(args, req.TenantID)
	argIndex++

	if req.EmployeeID != nil {
		conditions = append(conditions, fmt.Sprintf("employee_id = $%d", argIndex))
		args = append(args, *req.EmployeeID)
		argIndex++
	}

	if req.PatientName != nil {
		conditions = append(conditions, fmt.Sprintf("patient_name ILIKE $%d", argIndex))
		args = append(args, "%"+*req.PatientName+"%")
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, *req.StartDate)
		argIndex++
	}

	if req.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, *req.EndDate)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM medical_recordings WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count recordings: %w", err)
	}

	// Query recordings
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, employee_id, patient_name, patient_age, patient_gender, patient_phone,
		       recording_url, recording_duration, transcript_text, doctor_summary, therapist_summary,
		       consultant_summary, status, processing_error, recording_started_at, recording_ended_at,
		       processed_at, created_at, updated_at, deleted_at
		FROM medical_recordings
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query recordings: %w", err)
	}
	defer rows.Close()

	var recordings []*MedicalRecording
	for rows.Next() {
		var r MedicalRecording
		if err := rows.Scan(
			&r.ID,
			&r.TenantID,
			&r.EmployeeID,
			&r.PatientName,
			&r.PatientAge,
			&r.PatientGender,
			&r.PatientPhone,
			&r.RecordingURL,
			&r.RecordingDuration,
			&r.TranscriptText,
			&r.DoctorSummary,
			&r.TherapistSummary,
			&r.ConsultantSummary,
			&r.Status,
			&r.ProcessingError,
			&r.RecordingStartedAt,
			&r.RecordingEndedAt,
			&r.ProcessedAt,
			&r.CreatedAt,
			&r.UpdatedAt,
			&r.DeletedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan recording: %w", err)
		}
		recordings = append(recordings, &r)
	}

	return recordings, total, nil
}

// GetRecordingByID retrieves a medical recording by ID
func (s *Store) GetRecordingByID(ctx context.Context, id int64) (*MedicalRecording, error) {
	query := `
		SELECT id, tenant_id, employee_id, patient_name, patient_age, patient_gender, patient_phone,
		       recording_url, recording_duration, transcript_text, doctor_summary, therapist_summary,
		       consultant_summary, status, processing_error, recording_started_at, recording_ended_at,
		       processed_at, created_at, updated_at, deleted_at
		FROM medical_recordings
		WHERE id = $1 AND deleted_at IS NULL
	`

	var r MedicalRecording
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&r.ID,
		&r.TenantID,
		&r.EmployeeID,
		&r.PatientName,
		&r.PatientAge,
		&r.PatientGender,
		&r.PatientPhone,
		&r.RecordingURL,
		&r.RecordingDuration,
		&r.TranscriptText,
		&r.DoctorSummary,
		&r.TherapistSummary,
		&r.ConsultantSummary,
		&r.Status,
		&r.ProcessingError,
		&r.RecordingStartedAt,
		&r.RecordingEndedAt,
		&r.ProcessedAt,
		&r.CreatedAt,
		&r.UpdatedAt,
		&r.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("recording not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query recording: %w", err)
	}

	return &r, nil
}

// CreateRecording creates a new medical recording
func (s *Store) CreateRecording(ctx context.Context, req CreateRecordingRequest) (*MedicalRecording, error) {
	query := `
		INSERT INTO medical_recordings (
			tenant_id, employee_id, patient_name, patient_age, patient_gender, patient_phone,
			recording_url, recording_duration, recording_started_at, recording_ended_at,
			status, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
		RETURNING id, tenant_id, employee_id, patient_name, patient_age, patient_gender, patient_phone,
		          recording_url, recording_duration, transcript_text, doctor_summary, therapist_summary,
		          consultant_summary, status, processing_error, recording_started_at, recording_ended_at,
		          processed_at, created_at, updated_at, deleted_at
	`

	var r MedicalRecording
	err := s.pool.QueryRow(ctx, query,
		req.TenantID,
		req.EmployeeID,
		req.PatientName,
		req.PatientAge,
		req.PatientGender,
		req.PatientPhone,
		req.RecordingURL,
		req.RecordingDuration,
		req.RecordingStartedAt,
		req.RecordingEndedAt,
		StatusPending,
	).Scan(
		&r.ID,
		&r.TenantID,
		&r.EmployeeID,
		&r.PatientName,
		&r.PatientAge,
		&r.PatientGender,
		&r.PatientPhone,
		&r.RecordingURL,
		&r.RecordingDuration,
		&r.TranscriptText,
		&r.DoctorSummary,
		&r.TherapistSummary,
		&r.ConsultantSummary,
		&r.Status,
		&r.ProcessingError,
		&r.RecordingStartedAt,
		&r.RecordingEndedAt,
		&r.ProcessedAt,
		&r.CreatedAt,
		&r.UpdatedAt,
		&r.DeletedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create recording: %w", err)
	}

	return &r, nil
}

// UpdateRecording updates a medical recording
func (s *Store) UpdateRecording(ctx context.Context, id int64, req UpdateRecordingRequest) (*MedicalRecording, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.PatientName != nil {
		setClauses = append(setClauses, fmt.Sprintf("patient_name = $%d", argIndex))
		args = append(args, *req.PatientName)
		argIndex++
	}

	if req.PatientAge != nil {
		setClauses = append(setClauses, fmt.Sprintf("patient_age = $%d", argIndex))
		args = append(args, *req.PatientAge)
		argIndex++
	}

	if req.PatientGender != nil {
		setClauses = append(setClauses, fmt.Sprintf("patient_gender = $%d", argIndex))
		args = append(args, *req.PatientGender)
		argIndex++
	}

	if req.PatientPhone != nil {
		setClauses = append(setClauses, fmt.Sprintf("patient_phone = $%d", argIndex))
		args = append(args, *req.PatientPhone)
		argIndex++
	}

	if req.TranscriptText != nil {
		setClauses = append(setClauses, fmt.Sprintf("transcript_text = $%d", argIndex))
		args = append(args, *req.TranscriptText)
		argIndex++
	}

	if req.DoctorSummary != nil {
		setClauses = append(setClauses, fmt.Sprintf("doctor_summary = $%d", argIndex))
		args = append(args, *req.DoctorSummary)
		argIndex++
	}

	if req.TherapistSummary != nil {
		setClauses = append(setClauses, fmt.Sprintf("therapist_summary = $%d", argIndex))
		args = append(args, *req.TherapistSummary)
		argIndex++
	}

	if req.ConsultantSummary != nil {
		setClauses = append(setClauses, fmt.Sprintf("consultant_summary = $%d", argIndex))
		args = append(args, *req.ConsultantSummary)
		argIndex++
	}

	if req.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.ProcessingError != nil {
		setClauses = append(setClauses, fmt.Sprintf("processing_error = $%d", argIndex))
		args = append(args, *req.ProcessingError)
		argIndex++
	}

	if req.ProcessedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("processed_at = $%d", argIndex))
		args = append(args, *req.ProcessedAt)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetRecordingByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE medical_recordings
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, tenant_id, employee_id, patient_name, patient_age, patient_gender, patient_phone,
		          recording_url, recording_duration, transcript_text, doctor_summary, therapist_summary,
		          consultant_summary, status, processing_error, recording_started_at, recording_ended_at,
		          processed_at, created_at, updated_at, deleted_at
	`, strings.Join(setClauses, ", "), argIndex)

	var r MedicalRecording
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&r.ID,
		&r.TenantID,
		&r.EmployeeID,
		&r.PatientName,
		&r.PatientAge,
		&r.PatientGender,
		&r.PatientPhone,
		&r.RecordingURL,
		&r.RecordingDuration,
		&r.TranscriptText,
		&r.DoctorSummary,
		&r.TherapistSummary,
		&r.ConsultantSummary,
		&r.Status,
		&r.ProcessingError,
		&r.RecordingStartedAt,
		&r.RecordingEndedAt,
		&r.ProcessedAt,
		&r.CreatedAt,
		&r.UpdatedAt,
		&r.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("recording not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update recording: %w", err)
	}

	return &r, nil
}

// DeleteRecording soft deletes a medical recording
func (s *Store) DeleteRecording(ctx context.Context, id int64) error {
	query := `
		UPDATE medical_recordings
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete recording: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("recording not found")
	}

	return nil
}
// Recording Statistics Methods

// GetStatsOverview retrieves overview statistics
func (s *Store) GetStatsOverview(ctx context.Context, tenantID int64, startDate, endDate *string) (*RecordingStatsOverviewResponse, error) {
	query := `
		SELECT 
			COUNT(*) as total_recordings,
			COALESCE(SUM(recording_duration), 0) as total_duration,
			COUNT(CASE WHEN status = 'completed' THEN 1 END) as completed_recordings,
			COUNT(CASE WHEN status = 'pending' THEN 1 END) as pending_recordings,
			COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed_recordings,
			COALESCE(AVG(recording_duration), 0) as avg_duration,
			COUNT(CASE WHEN DATE(created_at) = CURRENT_DATE THEN 1 END) as today_recordings
		FROM medical_recordings
		WHERE tenant_id = $1 AND deleted_at IS NULL
	`
	
	var stats RecordingStatsOverviewResponse
	err := s.pool.QueryRow(ctx, query, tenantID).Scan(
		&stats.TotalRecordings,
		&stats.TotalDuration,
		&stats.CompletedRecordings,
		&stats.PendingRecordings,
		&stats.FailedRecordings,
		&stats.AvgDuration,
		&stats.TodayRecordings,
	)
	
	if err != nil {
		return nil, fmt.Errorf("failed to get stats overview: %w", err)
	}
	
	return &stats, nil
}

// GetStatsByScene retrieves statistics by scene
func (s *Store) GetStatsByScene(ctx context.Context, tenantID int64) ([]RecordingStatsBySceneResponse, error) {
	query := `
		SELECT 
			COALESCE(scene, 'unknown') as scene,
			COUNT(*) as count,
			COALESCE(SUM(recording_duration), 0) as duration,
			ROUND(COUNT(*) * 100.0 / SUM(COUNT(*)) OVER(), 2) as percentage
		FROM medical_recordings
		WHERE tenant_id = $1 AND deleted_at IS NULL
		GROUP BY scene
		ORDER BY count DESC
	`
	
	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats by scene: %w", err)
	}
	defer rows.Close()
	
	var stats []RecordingStatsBySceneResponse
	for rows.Next() {
		var stat RecordingStatsBySceneResponse
		if err := rows.Scan(&stat.Scene, &stat.Count, &stat.Duration, &stat.Percentage); err != nil {
			return nil, fmt.Errorf("failed to scan stat: %w", err)
		}
		stats = append(stats, stat)
	}
	
	return stats, nil
}

// GetStatsBySource retrieves statistics by source
func (s *Store) GetStatsBySource(ctx context.Context, tenantID int64) ([]RecordingStatsBySourceResponse, error) {
	query := `
		SELECT 
			COALESCE(source, 'unknown') as source,
			COUNT(*) as count,
			COALESCE(SUM(recording_duration), 0) as duration,
			ROUND(COUNT(*) * 100.0 / SUM(COUNT(*)) OVER(), 2) as percentage
		FROM medical_recordings
		WHERE tenant_id = $1 AND deleted_at IS NULL
		GROUP BY source
		ORDER BY count DESC
	`
	
	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats by source: %w", err)
	}
	defer rows.Close()
	
	var stats []RecordingStatsBySourceResponse
	for rows.Next() {
		var stat RecordingStatsBySourceResponse
		if err := rows.Scan(&stat.Source, &stat.Count, &stat.Duration, &stat.Percentage); err != nil {
			return nil, fmt.Errorf("failed to scan stat: %w", err)
		}
		stats = append(stats, stat)
	}
	
	return stats, nil
}

// Recording Task Methods

// ListRecordingTasks retrieves a paginated list of recording tasks
func (s *Store) ListRecordingTasks(ctx context.Context, req TaskListRequest) ([]*RecordingTask, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1
	
	conditions = append(conditions, "1=1")
	
	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}
	
	if req.RecordingID != nil {
		conditions = append(conditions, fmt.Sprintf("recording_id = $%d", argIndex))
		args = append(args, *req.RecordingID)
		argIndex++
	}
	
	if req.AssignedTo != nil {
		conditions = append(conditions, fmt.Sprintf("assigned_to = $%d", argIndex))
		args = append(args, *req.AssignedTo)
		argIndex++
	}
	
	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}
	
	if req.TaskType != nil {
		conditions = append(conditions, fmt.Sprintf("task_type = $%d", argIndex))
		args = append(args, *req.TaskType)
		argIndex++
	}
	
	whereClause := strings.Join(conditions, " AND ")
	
	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM recording_tasks WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count tasks: %w", err)
	}
	
	// Query tasks
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, recording_id, task_type, title, description, assigned_to, assigned_by,
		       status, due_date, completed_at, completed_by, cancelled_at, cancel_reason, created_at, updated_at
		FROM recording_tasks
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)
	
	args = append(args, req.PageSize, offset)
	
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()
	
	var tasks []*RecordingTask
	for rows.Next() {
		var t RecordingTask
		if err := rows.Scan(
			&t.ID, &t.TenantID, &t.RecordingID, &t.TaskType, &t.Title, &t.Description,
			&t.AssignedTo, &t.AssignedBy, &t.Status, &t.DueDate, &t.CompletedAt,
			&t.CompletedBy, &t.CancelledAt, &t.CancelReason, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, &t)
	}
	
	return tasks, total, nil
}

// GetTaskByID retrieves a recording task by ID
func (s *Store) GetTaskByID(ctx context.Context, id int64) (*RecordingTask, error) {
	query := `
		SELECT id, tenant_id, recording_id, task_type, title, description, assigned_to, assigned_by,
		       status, due_date, completed_at, completed_by, cancelled_at, cancel_reason, created_at, updated_at
		FROM recording_tasks
		WHERE id = $1
	`
	
	var t RecordingTask
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.TenantID, &t.RecordingID, &t.TaskType, &t.Title, &t.Description,
		&t.AssignedTo, &t.AssignedBy, &t.Status, &t.DueDate, &t.CompletedAt,
		&t.CompletedBy, &t.CancelledAt, &t.CancelReason, &t.CreatedAt, &t.UpdatedAt,
	)
	
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("task not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query task: %w", err)
	}
	
	return &t, nil
}

// CompleteTask marks a task as completed
func (s *Store) CompleteTask(ctx context.Context, id int64, completedBy int64) error {
	query := `
		UPDATE recording_tasks
		SET status = $1, completed_at = NOW(), completed_by = $2, updated_at = NOW()
		WHERE id = $3 AND status != $4
	`
	
	result, err := s.pool.Exec(ctx, query, TaskStatusCompleted, completedBy, id, TaskStatusCompleted)
	if err != nil {
		return fmt.Errorf("failed to complete task: %w", err)
	}
	
	if result.RowsAffected() == 0 {
		return fmt.Errorf("task not found or already completed")
	}
	
	return nil
}

// CancelTask marks a task as cancelled
func (s *Store) CancelTask(ctx context.Context, id int64, reason string) error {
	query := `
		UPDATE recording_tasks
		SET status = $1, cancelled_at = NOW(), cancel_reason = $2, updated_at = NOW()
		WHERE id = $3 AND status NOT IN ($4, $5)
	`
	
	result, err := s.pool.Exec(ctx, query, TaskStatusCancelled, reason, id, TaskStatusCompleted, TaskStatusCancelled)
	if err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}
	
	if result.RowsAffected() == 0 {
		return fmt.Errorf("task not found or cannot be cancelled")
	}
	
	return nil
}

// Recording Prompt Methods

// ListRecordingPrompts retrieves a paginated list of recording prompts
func (s *Store) ListRecordingPrompts(ctx context.Context, req RecordingPromptListRequest) ([]*RecordingPrompt, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1
	
	conditions = append(conditions, "deleted_at IS NULL")
	
	if req.Code != nil {
		conditions = append(conditions, fmt.Sprintf("code ILIKE $%d", argIndex))
		args = append(args, "%"+*req.Code+"%")
		argIndex++
	}
	
	if req.Name != nil {
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", argIndex))
		args = append(args, "%"+*req.Name+"%")
		argIndex++
	}
	
	if req.IsActive != nil {
		conditions = append(conditions, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}
	
	whereClause := strings.Join(conditions, " AND ")
	
	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM recording_prompts WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count prompts: %w", err)
	}
	
	// Query prompts
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, code, name, description, prompt_text, variables, is_active, created_at, updated_at
		FROM recording_prompts
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)
	
	args = append(args, req.PageSize, offset)
	
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query prompts: %w", err)
	}
	defer rows.Close()
	
	var prompts []*RecordingPrompt
	for rows.Next() {
		var p RecordingPrompt
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Description, &p.PromptText, &p.Variables, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan prompt: %w", err)
		}
		prompts = append(prompts, &p)
	}
	
	return prompts, total, nil
}

// GetRecordingPromptByCode retrieves a recording prompt by code
func (s *Store) GetRecordingPromptByCode(ctx context.Context, code string) (*RecordingPrompt, error) {
	query := `
		SELECT id, code, name, description, prompt_text, variables, is_active, created_at, updated_at
		FROM recording_prompts
		WHERE code = $1 AND deleted_at IS NULL
	`
	
	var p RecordingPrompt
	err := s.pool.QueryRow(ctx, query, code).Scan(
		&p.ID, &p.Code, &p.Name, &p.Description, &p.PromptText, &p.Variables, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
	)
	
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("prompt not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query prompt: %w", err)
	}
	
	return &p, nil
}

// CreateRecordingPrompt creates a new recording prompt
func (s *Store) CreateRecordingPrompt(ctx context.Context, req CreateRecordingPromptRequest) (*RecordingPrompt, error) {
	query := `
		INSERT INTO recording_prompts (code, name, description, prompt_text, variables, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		RETURNING id, code, name, description, prompt_text, variables, is_active, created_at, updated_at
	`
	
	var p RecordingPrompt
	err := s.pool.QueryRow(ctx, query, req.Code, req.Name, req.Description, req.PromptText, req.Variables, req.IsActive).Scan(
		&p.ID, &p.Code, &p.Name, &p.Description, &p.PromptText, &p.Variables, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
	)
	
	if err != nil {
		return nil, fmt.Errorf("failed to create prompt: %w", err)
	}
	
	return &p, nil
}

// UpdateRecordingPrompt updates a recording prompt
func (s *Store) UpdateRecordingPrompt(ctx context.Context, code string, req UpdateRecordingPromptRequest) (*RecordingPrompt, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1
	
	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *req.Name)
		argIndex++
	}
	
	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIndex))
		args = append(args, *req.Description)
		argIndex++
	}
	
	if req.PromptText != nil {
		setClauses = append(setClauses, fmt.Sprintf("prompt_text = $%d", argIndex))
		args = append(args, *req.PromptText)
		argIndex++
	}
	
	if req.Variables != nil {
		setClauses = append(setClauses, fmt.Sprintf("variables = $%d", argIndex))
		args = append(args, req.Variables)
		argIndex++
	}
	
	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}
	
	if len(setClauses) == 0 {
		return s.GetRecordingPromptByCode(ctx, code)
	}
	
	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, code)
	
	query := fmt.Sprintf(`
		UPDATE recording_prompts
		SET %s
		WHERE code = $%d AND deleted_at IS NULL
		RETURNING id, code, name, description, prompt_text, variables, is_active, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIndex)
	
	var p RecordingPrompt
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&p.ID, &p.Code, &p.Name, &p.Description, &p.PromptText, &p.Variables, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
	)
	
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("prompt not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update prompt: %w", err)
	}
	
	return &p, nil
}

// DeleteRecordingPrompt soft deletes a recording prompt
func (s *Store) DeleteRecordingPrompt(ctx context.Context, code string) error {
	query := `
		UPDATE recording_prompts
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE code = $1 AND deleted_at IS NULL
	`
	
	result, err := s.pool.Exec(ctx, query, code)
	if err != nil {
		return fmt.Errorf("failed to delete prompt: %w", err)
	}
	
	if result.RowsAffected() == 0 {
		return fmt.Errorf("prompt not found")
	}
	
	return nil
}

// Best Practice Methods

// ListBestPractices retrieves a list of best practices
func (s *Store) ListBestPractices(ctx context.Context, tenantID int64) ([]*RecordingBestPractice, error) {
	query := `
		SELECT id, tenant_id, recording_id, title, description, category, tags, created_by, created_at, updated_at
		FROM recording_best_practices
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`
	
	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query best practices: %w", err)
	}
	defer rows.Close()
	
	var practices []*RecordingBestPractice
	for rows.Next() {
		var p RecordingBestPractice
		if err := rows.Scan(&p.ID, &p.TenantID, &p.RecordingID, &p.Title, &p.Description, &p.Category, &p.Tags, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan best practice: %w", err)
		}
		practices = append(practices, &p)
	}
	
	return practices, nil
}

// AddBestPractice adds a recording to best practices
func (s *Store) AddBestPractice(ctx context.Context, tenantID, recordingID, createdBy int64, req AddBestPracticeRequest) (*RecordingBestPractice, error) {
	query := `
		INSERT INTO recording_best_practices (tenant_id, recording_id, title, description, category, tags, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		RETURNING id, tenant_id, recording_id, title, description, category, tags, created_by, created_at, updated_at
	`
	
	var p RecordingBestPractice
	err := s.pool.QueryRow(ctx, query, tenantID, recordingID, req.Title, req.Description, req.Category, req.Tags, createdBy).Scan(
		&p.ID, &p.TenantID, &p.RecordingID, &p.Title, &p.Description, &p.Category, &p.Tags, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	
	if err != nil {
		return nil, fmt.Errorf("failed to add best practice: %w", err)
	}
	
	return &p, nil
}

// DeleteBestPractice removes a recording from best practices
func (s *Store) DeleteBestPractice(ctx context.Context, recordingID int64) error {
	query := `
		UPDATE recording_best_practices
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE recording_id = $1 AND deleted_at IS NULL
	`
	
	result, err := s.pool.Exec(ctx, query, recordingID)
	if err != nil {
		return fmt.Errorf("failed to delete best practice: %w", err)
	}
	
	if result.RowsAffected() == 0 {
		return fmt.Errorf("best practice not found")
	}
	
	return nil
}
