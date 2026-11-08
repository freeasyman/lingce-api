package dealexpert

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// RecordingConsultant 返回一条接待的负责咨询师，用于数据范围校验。
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

// RecordingBrief 是任务草案所需的录音最小信息。
type RecordingBrief struct {
	EncounterID  int64  `json:"encounter_id"`
	CustomerID   int64  `json:"customer_id"`
	CustomerName string `json:"customer_name"`
}

func (s *Store) RecordingBrief(ctx context.Context, tenantID, recordingID int64) (*RecordingBrief, error) {
	var out RecordingBrief
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(encounter_id, 0), COALESCE(customer_id, 0), COALESCE(customer_name, '')
		FROM recordings WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, recordingID).Scan(&out.EncounterID, &out.CustomerID, &out.CustomerName)
	if err != nil {
		return nil, fmt.Errorf("recording brief: %w", err)
	}
	return &out, nil
}
