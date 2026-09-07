package encounter

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type RealtimeRequest struct {
	TenantID        int64
	ProviderID      int64
	DepartmentID    *int64
	ClientRequestID string
	PatientID       *int64
	PatientName     string
	StartedAt       time.Time
}

func (s *Store) GetRealtimeByRequestID(ctx context.Context, tenantID int64, clientRequestID string) (int64, bool, error) {
	var encounterID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id
		FROM encounters
		WHERE tenant_id=$1 AND source_type='realtime' AND source_id=$2
	`, tenantID, realtimeSourceID(clientRequestID)).Scan(&encounterID)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("load realtime encounter: %w", err)
	}
	return encounterID, true, nil
}

func (s *Store) EnsureRealtime(ctx context.Context, req RealtimeRequest) (int64, error) {
	if req.TenantID <= 0 || req.ProviderID <= 0 || strings.TrimSpace(req.ClientRequestID) == "" {
		return 0, fmt.Errorf("realtime encounter request is invalid")
	}
	startedAt := req.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	var encounterID int64
	err := s.pool.QueryRow(ctx, `
		WITH next_encounter AS (
			SELECT nextval(pg_get_serial_sequence('encounters', 'id')) AS id
		)
		INSERT INTO encounters (
			id, tenant_id, source_type, source_id, patient_id, patient_name,
			patient_candidate_json, provider_id, department_id, channel, visit_type,
			started_at, status, created_at, updated_at
		)
		SELECT id, $1, 'realtime', $2, $3, NULLIF($4, ''), '{}'::jsonb, $5, $6,
			'online', 'consultation', $7, 'new', NOW(), NOW()
		FROM next_encounter
		ON CONFLICT (tenant_id, source_type, source_id) DO UPDATE
		SET updated_at=encounters.updated_at
		RETURNING id
	`, req.TenantID, realtimeSourceID(req.ClientRequestID), req.PatientID, strings.TrimSpace(req.PatientName), req.ProviderID, req.DepartmentID, startedAt).Scan(&encounterID)
	if err != nil {
		return 0, fmt.Errorf("ensure realtime encounter: %w", err)
	}
	return encounterID, nil
}

func BindPatientTx(ctx context.Context, tx pgx.Tx, tenantID, encounterID int64, patientID *int64, patientName string) error {
	result, err := tx.Exec(ctx, `
		UPDATE encounters
		SET patient_id=$1, patient_name=NULLIF($2, ''), updated_at=NOW()
		WHERE tenant_id=$3 AND id=$4
	`, patientID, strings.TrimSpace(patientName), tenantID, encounterID)
	if err != nil {
		return fmt.Errorf("bind shared encounter patient: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("shared encounter not found")
	}
	return nil
}

func realtimeSourceID(clientRequestID string) int64 {
	digest := sha256.Sum256([]byte(clientRequestID))
	value := int64(binary.BigEndian.Uint64(digest[:8]) & 0x7fffffffffffffff)
	if value == 0 {
		return 1
	}
	return value
}
