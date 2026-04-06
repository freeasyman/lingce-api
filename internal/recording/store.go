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
