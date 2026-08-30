package emrprocess

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Append(ctx context.Context, req AppendRequest) (*ProcessRecord, error) {
	return appendRecord(ctx, s.pool, req)
}

func (s *Store) AppendTx(ctx context.Context, tx pgx.Tx, req AppendRequest) (*ProcessRecord, error) {
	return appendRecord(ctx, tx, req)
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func appendRecord(ctx context.Context, db queryRower, req AppendRequest) (*ProcessRecord, error) {
	output, err := json.Marshal(req.OutputInfo)
	if err != nil {
		return nil, fmt.Errorf("marshal process output: %w", err)
	}
	changes, err := json.Marshal(req.ContentChanges)
	if err != nil {
		return nil, fmt.Errorf("marshal process changes: %w", err)
	}
	item := &ProcessRecord{}
	var outputRaw, changesRaw []byte
	err = db.QueryRow(ctx, `INSERT INTO emr_process_records
	(tenant_id, record_id, action_type, action_result, actor_type, actor_id, source, before_status, after_status,
	 action_snapshot_id, before_snapshot_id, after_snapshot_id, check_run_id, ai_candidate_id, output_info,
	 failure_reason, action_note, content_changes)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,$16,$17,$18::jsonb)
	RETURNING id, tenant_id, record_id, action_type, action_result, occurred_at, actor_type, actor_id, source,
	 before_status, after_status, action_snapshot_id, before_snapshot_id, after_snapshot_id, check_run_id,
	 ai_candidate_id, output_info, failure_reason, action_note, content_changes`,
		req.TenantID, req.RecordID, req.ActionType, req.ActionResult, req.ActorType, req.ActorID, req.Source,
		req.BeforeStatus, req.AfterStatus, req.ActionSnapshotID, req.BeforeSnapshotID, req.AfterSnapshotID,
		req.CheckRunID, req.AICandidateID, string(output), req.FailureReason, req.ActionNote, string(changes)).Scan(
		&item.ID, &item.TenantID, &item.RecordID, &item.ActionType, &item.ActionResult, &item.OccurredAt,
		&item.ActorType, &item.ActorID, &item.Source, &item.BeforeStatus, &item.AfterStatus,
		&item.ActionSnapshotID, &item.BeforeSnapshotID, &item.AfterSnapshotID, &item.CheckRunID,
		&item.AICandidateID, &outputRaw, &item.FailureReason, &item.ActionNote, &changesRaw)
	if err != nil {
		return nil, fmt.Errorf("append emr process record: %w", err)
	}
	item.OutputInfo = map[string]any{}
	item.ContentChanges = []any{}
	_ = json.Unmarshal(outputRaw, &item.OutputInfo)
	_ = json.Unmarshal(changesRaw, &item.ContentChanges)
	return item, nil
}

func (s *Store) List(ctx context.Context, tenantID int64, recordID string) ([]*ProcessRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, record_id, action_type, action_result, occurred_at,
	 actor_type, actor_id, source, before_status, after_status, action_snapshot_id, before_snapshot_id,
	 after_snapshot_id, check_run_id, ai_candidate_id, output_info, failure_reason, action_note, content_changes
	 FROM emr_process_records WHERE tenant_id=$1 AND record_id=$2 ORDER BY occurred_at DESC, id DESC`, tenantID, recordID)
	if err != nil {
		return nil, fmt.Errorf("list emr process records: %w", err)
	}
	defer rows.Close()
	items := make([]*ProcessRecord, 0)
	for rows.Next() {
		item := &ProcessRecord{}
		var outputRaw, changesRaw []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RecordID, &item.ActionType, &item.ActionResult, &item.OccurredAt,
			&item.ActorType, &item.ActorID, &item.Source, &item.BeforeStatus, &item.AfterStatus,
			&item.ActionSnapshotID, &item.BeforeSnapshotID, &item.AfterSnapshotID, &item.CheckRunID,
			&item.AICandidateID, &outputRaw, &item.FailureReason, &item.ActionNote, &changesRaw); err != nil {
			return nil, err
		}
		item.OutputInfo = map[string]any{}
		item.ContentChanges = []any{}
		_ = json.Unmarshal(outputRaw, &item.OutputInfo)
		_ = json.Unmarshal(changesRaw, &item.ContentChanges)
		items = append(items, item)
	}
	return items, rows.Err()
}

type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }
func (s *Service) Append(ctx context.Context, req AppendRequest) (*ProcessRecord, error) {
	return s.store.Append(ctx, req)
}
func (s *Service) AppendTx(ctx context.Context, tx pgx.Tx, req AppendRequest) (*ProcessRecord, error) {
	return s.store.AppendTx(ctx, tx, req)
}
func (s *Service) List(ctx context.Context, tenantID int64, recordID string) ([]*ProcessRecord, error) {
	return s.store.List(ctx, tenantID, recordID)
}
