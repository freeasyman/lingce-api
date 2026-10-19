package dealexpert

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Receive(ctx context.Context, req ReceiveRequest) (*Analysis, bool, error) {
	if s == nil || s.pool == nil {
		return nil, false, fmt.Errorf("deal expert store is nil")
	}
	var row Analysis
	var customerID *int64
	var recordedAt *time.Time
	var inserted bool
	err := s.pool.QueryRow(ctx, `
		INSERT INTO deal_expert_analyses (
		 tenant_id, recording_id, encounter_id, employee_id, customer_id, customer_name,
		 business_scope, role_code, recorded_at, cleaned_transcript,
		 analysis_pipeline_version, prompt_version, status
		) VALUES ($1,$2,$3,$4,NULLIF($5,0),$6,$7,$8,NULLIF($9,'')::timestamptz,$10,$11,$12,'received')
		ON CONFLICT (tenant_id, recording_id, analysis_pipeline_version, prompt_version)
		DO UPDATE SET updated_at = NOW()
		RETURNING id, tenant_id, recording_id, encounter_id, employee_id, customer_id, customer_name,
		 business_scope, role_code, recorded_at, analysis_pipeline_version, prompt_version,
		 status, received_at, updated_at,
		 (xmax = 0) AS inserted
	`, req.TenantID, req.RecordingID, req.EncounterID, req.EmployeeID, req.CustomerID, req.CustomerName,
		req.BusinessScope, req.RoleCode, req.RecordedAt, req.CleanedTranscript, req.AnalysisPipelineVersion, req.PromptVersion).
		Scan(&row.ID, &row.TenantID, &row.RecordingID, &row.EncounterID, &row.EmployeeID, &customerID, &row.CustomerName,
			&row.BusinessScope, &row.RoleCode, &recordedAt, &row.AnalysisPipelineVersion, &row.PromptVersion,
			&row.Status, &row.ReceivedAt, &row.UpdatedAt, &inserted)
	if err != nil {
		return nil, false, fmt.Errorf("receive deal expert analysis: %w", err)
	}
	row.CustomerID, row.RecordedAt = customerID, recordedAt
	return &row, inserted, nil
}
