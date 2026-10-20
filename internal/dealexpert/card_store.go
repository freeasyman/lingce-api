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
