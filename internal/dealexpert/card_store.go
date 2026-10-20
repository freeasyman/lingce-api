package dealexpert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// EncounterCard 是前端读取的“经营结案卡”：只返回卡片本身与状态，
// 不返回原始模型响应（可能包含患者原话与调试信息）。
type EncounterCard struct {
	RecordingID   int64          `json:"recording_id"`
	Status        string         `json:"status"`
	Card          map[string]any `json:"card,omitempty"`
	AnalyzedAt    *time.Time     `json:"analyzed_at,omitempty"`
	ModelCode     string         `json:"model_code,omitempty"`
	PromptVersion string         `json:"prompt_version,omitempty"`
	ErrorMessage  string         `json:"error_message,omitempty"`
}

func (s *Store) LatestCard(ctx context.Context, tenantID, recordingID int64) (*EncounterCard, error) {
	var row EncounterCard
	var payload []byte
	err := s.pool.QueryRow(ctx, `
		SELECT recording_id, status, COALESCE(result::text, '{}'), analyzed_at,
		       COALESCE(model_code, ''), COALESCE(prompt_version, ''), COALESCE(error_message, '')
		FROM deal_expert_analyses
		WHERE tenant_id = $1 AND recording_id = $2
		ORDER BY analyzed_at DESC NULLS LAST, id DESC
		LIMIT 1
	`, tenantID, recordingID).Scan(&row.RecordingID, &row.Status, &payload, &row.AnalyzedAt, &row.ModelCode, &row.PromptVersion, &row.ErrorMessage)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCardNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load encounter card: %w", err)
	}
	if strings.TrimSpace(row.ErrorMessage) != "" {
		row.ErrorMessage = sanitizeCardError(row.ErrorMessage)
	}
	if row.Status == "analyzed" && len(payload) > 0 {
		var card map[string]any
		if err := json.Unmarshal(payload, &card); err != nil {
			return nil, fmt.Errorf("decode encounter card: %w", err)
		}
		row.Card = card
	}
	return &row, nil
}

// sanitizeCardError 去掉原始模型响应，只保留错误摘要。
func sanitizeCardError(message string) string {
	if idx := strings.Index(message, "-- raw response --"); idx >= 0 {
		message = message[:idx]
	}
	if len([]rune(message)) > 500 {
		return string([]rune(message)[:500]) + "..."
	}
	return strings.TrimSpace(message)
}

func (s *Store) RecordingConsultant(ctx context.Context, tenantID, recordingID int64) (int64, error) {
	var employeeID int64
	err := s.pool.QueryRow(ctx, `
		SELECT employee_id FROM recordings
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, recordingID).Scan(&employeeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("recording not found")
	}
	if err != nil {
		return 0, fmt.Errorf("query recording consultant: %w", err)
	}
	return employeeID, nil
}

// EnqueueAnalysis 为指定录音补生成结案卡；已有分析行时重置为待分析。
func (s *Store) EnqueueAnalysis(ctx context.Context, tenantID, recordingID int64) (string, error) {
	var encounterID, employeeID, customerID *int64
	var customerName, transcript string
	var recordedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT r.encounter_id, r.employee_id, r.customer_id, COALESCE(c.name, ''),
		       r.recorded_at, COALESCE(r.transcription_text, '')
		FROM recordings r LEFT JOIN customers c ON c.id = r.customer_id
		WHERE r.tenant_id = $1 AND r.id = $2 AND r.deleted_at IS NULL
	`, tenantID, recordingID).Scan(&encounterID, &employeeID, &customerID, &customerName, &recordedAt, &transcript)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("recording not found")
	}
	if err != nil {
		return "", fmt.Errorf("load recording: %w", err)
	}
	if len([]rune(strings.TrimSpace(transcript))) < 100 {
		return "", ErrNoTranscript
	}

	var existingID int64
	err = s.pool.QueryRow(ctx, `
		SELECT id FROM deal_expert_analyses
		WHERE tenant_id = $1 AND recording_id = $2
		  AND analysis_pipeline_version = 'recording-analysis-v1'
		ORDER BY id DESC LIMIT 1
	`, tenantID, recordingID).Scan(&existingID)
	if err == nil {
		_, err = s.pool.Exec(ctx, `
			UPDATE deal_expert_analyses
			SET status = 'received', attempts = 0, error_message = '', prompt_version = 'deal-expert-prompt-v1.2', updated_at = NOW()
			WHERE id = $1
		`, existingID)
		if err != nil {
			return "", fmt.Errorf("reset analysis: %w", err)
		}
		return "received", nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("find analysis: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO deal_expert_analyses
		  (tenant_id, recording_id, encounter_id, employee_id, customer_id, customer_name,
		   business_scope, role_code, recorded_at, cleaned_transcript,
		   analysis_pipeline_version, prompt_version, status)
		VALUES ($1,$2,COALESCE($3,0),COALESCE($4,0),$5,$6,'consultant','consultant',$7,$8,
		        'recording-analysis-v1','deal-expert-prompt-v1.2','received')
	`, tenantID, recordingID, encounterID, employeeID, customerID, customerName, recordedAt, transcript)
	if err != nil {
		return "", fmt.Errorf("enqueue analysis: %w", err)
	}
	return "received", nil
}
