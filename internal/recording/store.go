package recording

import (
	"context"
	"fmt"
	"strings"
	"time"

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

	conditions = append(conditions, "1=1")

	if len(req.TenantIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("r.tenant_id = ANY($%d)", argIndex))
		args = append(args, req.TenantIDs)
		argIndex++
	} else if req.TenantID > 0 {
		conditions = append(conditions, fmt.Sprintf("r.tenant_id = $%d", argIndex))
		args = append(args, req.TenantID)
		argIndex++
	}

	if req.EmployeeID != nil {
		conditions = append(conditions, fmt.Sprintf("r.employee_id = $%d", argIndex))
		args = append(args, *req.EmployeeID)
		argIndex++
	}

	if req.PatientName != nil {
		conditions = append(conditions, fmt.Sprintf("COALESCE(c.name, '') ILIKE $%d", argIndex))
		args = append(args, "%"+*req.PatientName+"%")
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf(`
			(CASE
				WHEN r.analysis_status = 'completed' THEN 'completed'
				WHEN r.analysis_status = 'failed' OR r.transcription_status = 'failed' THEN 'failed'
				WHEN r.analysis_status = 'pending' OR r.transcription_status = 'pending' THEN 'pending'
				ELSE 'processing'
			END) = $%d`, argIndex))
		args = append(args, string(*req.Status))
		argIndex++
	}

	if req.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("r.created_at >= $%d", argIndex))
		args = append(args, *req.StartDate)
		argIndex++
	}

	if req.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("r.created_at <= $%d", argIndex))
		args = append(args, *req.EndDate)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE %s
	`, whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count recordings: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT
			r.id,
			r.tenant_id,
			r.employee_id,
			COALESCE(c.name, '') AS patient_name,
			c.age AS patient_age,
			c.gender AS patient_gender,
			c.phone AS patient_phone,
			r.file_url AS recording_url,
			r.duration AS recording_duration,
			r.transcription_text AS transcript_text,
			NULLIF(r.analysis_display->>'doctor_summary', '') AS doctor_summary,
			NULLIF(r.analysis_display->>'therapist_summary', '') AS therapist_summary,
			NULLIF(r.analysis_display->>'consultant_summary', '') AS consultant_summary,
			CASE
				WHEN r.analysis_status = 'completed' THEN 'completed'
				WHEN r.analysis_status = 'failed' OR r.transcription_status = 'failed' THEN 'failed'
				WHEN r.analysis_status = 'pending' OR r.transcription_status = 'pending' THEN 'pending'
				ELSE 'processing'
			END AS status,
			NULL::text AS processing_error,
			r.recorded_at AS recording_started_at,
			NULL::timestamp AS recording_ended_at,
			CASE WHEN r.analysis_status = 'completed' THEN COALESCE(r.updated_at, r.created_at, NOW()) ELSE NULL::timestamp END AS processed_at,
			r.created_at,
			COALESCE(r.updated_at, r.created_at, NOW()) AS updated_at,
			NULL::timestamp AS deleted_at
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE %s
		ORDER BY r.created_at DESC
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
		SELECT
			r.id,
			r.tenant_id,
			r.employee_id,
			COALESCE(c.name, '') AS patient_name,
			c.age AS patient_age,
			c.gender AS patient_gender,
			c.phone AS patient_phone,
			r.file_url AS recording_url,
			r.duration AS recording_duration,
			r.transcription_text AS transcript_text,
			NULLIF(r.analysis_display->>'doctor_summary', '') AS doctor_summary,
			NULLIF(r.analysis_display->>'therapist_summary', '') AS therapist_summary,
			NULLIF(r.analysis_display->>'consultant_summary', '') AS consultant_summary,
			CASE
				WHEN r.analysis_status = 'completed' THEN 'completed'
				WHEN r.analysis_status = 'failed' OR r.transcription_status = 'failed' THEN 'failed'
				WHEN r.analysis_status = 'pending' OR r.transcription_status = 'pending' THEN 'pending'
				ELSE 'processing'
			END AS status,
			NULL::text AS processing_error,
			r.recorded_at AS recording_started_at,
			NULL::timestamp AS recording_ended_at,
			CASE WHEN r.analysis_status = 'completed' THEN COALESCE(r.updated_at, r.created_at, NOW()) ELSE NULL::timestamp END AS processed_at,
			r.created_at,
			COALESCE(r.updated_at, r.created_at, NOW()) AS updated_at,
			NULL::timestamp AS deleted_at
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE r.id = $1
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
	fileName := req.RecordingURL
	if idx := strings.LastIndex(fileName, "/"); idx >= 0 && idx < len(fileName)-1 {
		fileName = fileName[idx+1:]
	}
	if fileName == "" {
		fileName = "recording.wav"
	}

	query := `
		INSERT INTO recordings (
			tenant_id, employee_id, file_url, file_name, duration, mime_type,
			source, scene, notes, status, transcription_status, analysis_status,
			recorded_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, 'audio/wav', 'manual', 'consultation', $6, 'uploaded', 'pending', 'pending', NOW(), NOW(), NOW())
		RETURNING id
	`
	var newID int64
	err := s.pool.QueryRow(ctx, query,
		req.TenantID,
		req.EmployeeID,
		req.RecordingURL,
		fileName,
		req.RecordingDuration,
		req.PatientName,
	).Scan(&newID)

	if err != nil {
		return nil, fmt.Errorf("failed to create recording: %w", err)
	}
	return s.GetRecordingByID(ctx, newID)
}

// UpdateRecording updates a medical recording
func (s *Store) UpdateRecording(ctx context.Context, id int64, req UpdateRecordingRequest) (*MedicalRecording, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.TranscriptText != nil {
		setClauses = append(setClauses, fmt.Sprintf("transcription_text = $%d", argIndex))
		args = append(args, *req.TranscriptText)
		argIndex++
	}

	if req.Status != nil {
		if *req.Status == StatusCompleted {
			setClauses = append(setClauses, fmt.Sprintf("analysis_status = $%d", argIndex))
			args = append(args, "completed")
			argIndex++
			setClauses = append(setClauses, fmt.Sprintf("transcription_status = $%d", argIndex))
			args = append(args, "completed")
			argIndex++
		} else if *req.Status == StatusFailed {
			setClauses = append(setClauses, fmt.Sprintf("analysis_status = $%d", argIndex))
			args = append(args, "failed")
			argIndex++
		} else {
			setClauses = append(setClauses, fmt.Sprintf("analysis_status = $%d", argIndex))
			args = append(args, "pending")
			argIndex++
		}
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, "uploaded")
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetRecordingByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE recordings
		SET %s
		WHERE id = $%d
	`, strings.Join(setClauses, ", "), argIndex)
	_, err := s.pool.Exec(ctx, query, args...)

	if err != nil {
		return nil, fmt.Errorf("failed to update recording: %w", err)
	}
	return s.GetRecordingByID(ctx, id)
}

// DeleteRecording deletes a recording
func (s *Store) DeleteRecording(ctx context.Context, id int64) error {
	query := `DELETE FROM recordings WHERE id = $1`

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
			COALESCE(SUM(duration), 0) as total_duration,
			COUNT(CASE WHEN analysis_status = 'completed' THEN 1 END) as completed_recordings,
			COUNT(CASE WHEN analysis_status = 'pending' THEN 1 END) as pending_recordings,
			COUNT(CASE WHEN analysis_status = 'failed' OR transcription_status = 'failed' THEN 1 END) as failed_recordings,
			COALESCE(AVG(duration), 0) as avg_duration,
			COUNT(CASE WHEN DATE(created_at) = CURRENT_DATE THEN 1 END) as today_recordings
		FROM recordings
		WHERE tenant_id = $1
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
			COALESCE(SUM(duration), 0) as duration,
			ROUND(COUNT(*) * 100.0 / SUM(COUNT(*)) OVER(), 2) as percentage
		FROM recordings
		WHERE tenant_id = $1
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
			COALESCE(SUM(duration), 0) as duration,
			ROUND(COUNT(*) * 100.0 / SUM(COUNT(*)) OVER(), 2) as percentage
		FROM recordings
		WHERE tenant_id = $1
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

	if len(req.TenantIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("tenant_id = ANY($%d)", argIndex))
		args = append(args, req.TenantIDs)
		argIndex++
	} else if req.TenantID != nil {
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
		conditions = append(conditions, fmt.Sprintf("source_type = $%d", argIndex))
		args = append(args, string(*req.TaskType))
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
		SELECT id, tenant_id, recording_id, source_type AS task_type, title, description, assigned_to, NULL::bigint AS assigned_by,
		       status, due_at AS due_date, completed_at, NULL::bigint AS completed_by, NULL::timestamp AS cancelled_at, NULL::text AS cancel_reason, created_at, updated_at
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
		SELECT id, tenant_id, recording_id, source_type AS task_type, title, description, assigned_to, NULL::bigint AS assigned_by,
		       status, due_at AS due_date, completed_at, NULL::bigint AS completed_by, NULL::timestamp AS cancelled_at, NULL::text AS cancel_reason, created_at, updated_at
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
		SET status = $1, completed_at = NOW(), feedback = CONCAT(COALESCE(feedback, ''), CASE WHEN COALESCE(feedback, '') = '' THEN '' ELSE E'\n' END, 'completed_by=', $2::text), updated_at = NOW()
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
		SET status = $1, feedback = $2, updated_at = NOW()
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

// GetTaskStats retrieves task statistics for a tenant
func (s *Store) GetTaskStats(ctx context.Context, tenantID int64, tenantIDs []int64, assignedTo *int64) (*RecordingTaskStatsResponse, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	if len(tenantIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("tenant_id = ANY($%d)", argIndex))
		args = append(args, tenantIDs)
		argIndex++
	} else {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, tenantID)
		argIndex++
	}

	if assignedTo != nil {
		conditions = append(conditions, fmt.Sprintf("assigned_to = $%d", argIndex))
		args = append(args, *assignedTo)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			COUNT(*) AS total_tasks,
			COUNT(CASE WHEN status = 'pending' THEN 1 END) AS pending_tasks,
			COUNT(CASE WHEN status = 'assigned' THEN 1 END) AS assigned_tasks,
			COUNT(CASE WHEN status = 'completed' THEN 1 END) AS completed_tasks,
			COUNT(CASE WHEN status = 'cancelled' THEN 1 END) AS cancelled_tasks,
			COUNT(CASE
				WHEN due_at < NOW() AND status NOT IN ('completed', 'cancelled')
				THEN 1
			END) AS overdue_tasks
		FROM recording_tasks
		WHERE %s
	`, whereClause)

	var stats RecordingTaskStatsResponse
	if err := s.pool.QueryRow(ctx, query, args...).Scan(
		&stats.TotalTasks,
		&stats.PendingTasks,
		&stats.AssignedTasks,
		&stats.CompletedTasks,
		&stats.CancelledTasks,
		&stats.OverdueTasks,
	); err != nil {
		return nil, fmt.Errorf("failed to get task stats: %w", err)
	}

	return &stats, nil
}

// GetDailyBriefing retrieves daily task briefing for a tenant
func (s *Store) GetDailyBriefing(ctx context.Context, tenantID int64, assignedTo *int64, date time.Time) (*DailyBriefingResponse, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
	args = append(args, tenantID)
	argIndex++

	if assignedTo != nil {
		conditions = append(conditions, fmt.Sprintf("assigned_to = $%d", argIndex))
		args = append(args, *assignedTo)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	// Reuse day bounds multiple times in the same query.
	startIdx1, endIdx1 := argIndex, argIndex+1
	startIdx2, endIdx2 := argIndex+2, argIndex+3
	endIdx3 := argIndex + 4
	args = append(args, dayStart, dayEnd, dayStart, dayEnd, dayEnd)

	query := fmt.Sprintf(`
		SELECT
			COUNT(CASE WHEN created_at >= $%d AND created_at < $%d THEN 1 END) AS today_tasks,
			COUNT(CASE WHEN status = 'completed' AND completed_at >= $%d AND completed_at < $%d THEN 1 END) AS completed_tasks,
			COUNT(CASE WHEN status IN ('pending', 'assigned') THEN 1 END) AS pending_tasks,
			COUNT(CASE
				WHEN due_at IS NOT NULL
					AND due_at < $%d
					AND status IN ('pending', 'assigned')
				THEN 1
			END) AS high_priority_tasks
		FROM recording_tasks
		WHERE %s
	`, startIdx1, endIdx1, startIdx2, endIdx2, endIdx3, whereClause)

	briefing := &DailyBriefingResponse{
		Date: dayStart.Format("2006-01-02"),
	}
	if err := s.pool.QueryRow(ctx, query, args...).Scan(
		&briefing.TodayTasks,
		&briefing.CompletedTasks,
		&briefing.PendingTasks,
		&briefing.HighPriorityTasks,
	); err != nil {
		return nil, fmt.Errorf("failed to get daily briefing: %w", err)
	}

	briefing.Summary = fmt.Sprintf(
		"今日新增%d项任务，完成%d项，待处理%d项，高优先级%d项。",
		briefing.TodayTasks,
		briefing.CompletedTasks,
		briefing.PendingTasks,
		briefing.HighPriorityTasks,
	)

	return briefing, nil
}

// Recording Prompt Methods

// ListRecordingPrompts retrieves a paginated list of recording prompts
func (s *Store) ListRecordingPrompts(ctx context.Context, req RecordingPromptListRequest) ([]*RecordingPrompt, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "1=1")

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
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM recording_analysis_prompts WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count prompts: %w", err)
	}

	// Query prompts
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, code, name, description, user_prompt_template AS prompt_text, '[]'::json AS variables, is_active, created_at, updated_at
		FROM recording_analysis_prompts
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
		SELECT id, code, name, description, user_prompt_template AS prompt_text, '[]'::json AS variables, is_active, created_at, updated_at
		FROM recording_analysis_prompts
		WHERE code = $1
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
		INSERT INTO recording_analysis_prompts (code, name, description, category, system_prompt, user_prompt_template, output_schema, version, is_active, created_by, updated_by, created_at, updated_at)
		VALUES ($1, $2, $3, 'default', '', $4, '{}'::json, 'v1', $5, 1, 1, NOW(), NOW())
		RETURNING id, code, name, description, user_prompt_template AS prompt_text, '[]'::json AS variables, is_active, created_at, updated_at
	`

	var p RecordingPrompt
	err := s.pool.QueryRow(ctx, query, req.Code, req.Name, req.Description, req.PromptText, req.IsActive).Scan(
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
		setClauses = append(setClauses, fmt.Sprintf("user_prompt_template = $%d", argIndex))
		args = append(args, *req.PromptText)
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
		UPDATE recording_analysis_prompts
		SET %s
		WHERE code = $%d
		RETURNING id, code, name, description, user_prompt_template AS prompt_text, '[]'::json AS variables, is_active, created_at, updated_at
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
		DELETE FROM recording_analysis_prompts
		WHERE code = $1
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
		SELECT id, tenant_id, recording_id, dimension AS title, note AS description, NULL::text AS category, '[]'::json AS tags, created_by, created_at, created_at AS updated_at
		FROM recording_best_practices
		WHERE tenant_id = $1
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
		INSERT INTO recording_best_practices (tenant_id, recording_id, dimension, note, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		RETURNING id, tenant_id, recording_id, dimension AS title, note AS description, NULL::text AS category, '[]'::json AS tags, created_by, created_at, created_at AS updated_at
	`

	var p RecordingBestPractice
	err := s.pool.QueryRow(ctx, query, tenantID, recordingID, req.Title, req.Description, createdBy).Scan(
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
		DELETE FROM recording_best_practices
		WHERE recording_id = $1
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
