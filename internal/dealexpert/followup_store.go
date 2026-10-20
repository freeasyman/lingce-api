package dealexpert

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const followUpColumns = `id, tenant_id, encounter_id, recording_id, customer_id, customer_name,
	objective, action, suggested_content, owner_employee_id, owner_name, due_at,
	status, result, result_note, next_action, created_at, updated_at, closed_at`

const outcomeColumns = `id, tenant_id, encounter_id, recording_id, type, amount, occurred_at,
	source, source_ref, note, confirmed_by, created_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFollowUp(row rowScanner) (*FollowUp, error) {
	var item FollowUp
	err := row.Scan(&item.ID, &item.TenantID, &item.EncounterID, &item.RecordingID, &item.CustomerID,
		&item.CustomerName, &item.Objective, &item.Action, &item.SuggestedContent,
		&item.OwnerEmployeeID, &item.OwnerName, &item.DueAt, &item.Status, &item.Result,
		&item.ResultNote, &item.NextAction, &item.CreatedAt, &item.UpdatedAt, &item.ClosedAt)
	return &item, err
}

func scanOutcome(row rowScanner) (*OutcomeEvent, error) {
	var item OutcomeEvent
	err := row.Scan(&item.ID, &item.TenantID, &item.EncounterID, &item.RecordingID, &item.Type,
		&item.Amount, &item.OccurredAt, &item.Source, &item.SourceRef, &item.Note,
		&item.ConfirmedBy, &item.CreatedAt)
	return &item, err
}

func (s *Store) CreateFollowUp(ctx context.Context, tenantID int64, req CreateFollowUpRequest, dueAt *time.Time) (*FollowUp, bool, error) {
	if s == nil || s.pool == nil {
		return nil, false, fmt.Errorf("deal expert store is nil")
	}
	const query = `
		INSERT INTO deal_follow_ups (
			tenant_id, encounter_id, recording_id, customer_id, customer_name,
			objective, action, suggested_content, owner_employee_id, owner_name, due_at, status
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'` + FollowUpPending + `')
		ON CONFLICT (tenant_id, encounter_id)
			WHERE status IN ('` + FollowUpPending + `','` + FollowUpProcessing + `','` + FollowUpWaitingCustomer + `','` + FollowUpWaitingInternal + `','` + FollowUpPaused + `')
			DO NOTHING
		RETURNING ` + followUpColumns

	item, err := scanFollowUp(s.pool.QueryRow(ctx, query,
		tenantID, req.EncounterID, req.RecordingID, req.CustomerID, req.CustomerName,
		req.Objective, req.Action, req.SuggestedContent, req.OwnerEmployeeID, req.OwnerName, dueAt))
	if err == nil {
		return item, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("create follow up: %w", err)
	}

	// 已有活动任务：不新建，返回原任务，由前端提示「已存在」。
	existing, err := s.getActiveFollowUp(ctx, tenantID, req.EncounterID)
	if err != nil {
		return nil, false, err
	}
	return existing, false, nil
}

func (s *Store) getActiveFollowUp(ctx context.Context, tenantID, encounterID int64) (*FollowUp, error) {
	query := `SELECT ` + followUpColumns + `
		FROM deal_follow_ups
		WHERE tenant_id = $1 AND encounter_id = $2 AND status = ANY($3)
		ORDER BY id DESC LIMIT 1`
	item, err := scanFollowUp(s.pool.QueryRow(ctx, query, tenantID, encounterID, activeStatuses()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFollowUpNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get active follow up: %w", err)
	}
	return item, nil
}

func (s *Store) GetFollowUp(ctx context.Context, tenantID, id int64) (*FollowUp, error) {
	query := `SELECT ` + followUpColumns + ` FROM deal_follow_ups WHERE tenant_id = $1 AND id = $2`
	item, err := scanFollowUp(s.pool.QueryRow(ctx, query, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFollowUpNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get follow up: %w", err)
	}
	return item, nil
}

func (s *Store) ListFollowUpsByEncounter(ctx context.Context, tenantID, encounterID int64) ([]*FollowUp, error) {
	query := `SELECT ` + followUpColumns + `
		FROM deal_follow_ups
		WHERE tenant_id = $1 AND encounter_id = $2
		ORDER BY id DESC`
	rows, err := s.pool.Query(ctx, query, tenantID, encounterID)
	if err != nil {
		return nil, fmt.Errorf("list encounter follow ups: %w", err)
	}
	defer rows.Close()

	items := make([]*FollowUp, 0, 4)
	for rows.Next() {
		item, err := scanFollowUp(rows)
		if err != nil {
			return nil, fmt.Errorf("scan follow up: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListFollowUps(ctx context.Context, tenantID int64, status string, page, pageSize int) ([]*FollowUp, int, error) {
	where := "tenant_id = $1"
	args := []any{tenantID}
	if status == "" {
		where += " AND status = ANY($2)"
		args = append(args, activeStatuses())
	} else {
		where += " AND status = $2"
		args = append(args, status)
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM deal_follow_ups WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count follow ups: %w", err)
	}

	query := fmt.Sprintf(`SELECT %s FROM deal_follow_ups WHERE %s
		ORDER BY due_at NULLS LAST, id DESC LIMIT $%d OFFSET $%d`, followUpColumns, where, len(args)+1, len(args)+2)
	rows, err := s.pool.Query(ctx, query, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list follow ups: %w", err)
	}
	defer rows.Close()

	items := make([]*FollowUp, 0, pageSize)
	for rows.Next() {
		item, err := scanFollowUp(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan follow up: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) CompleteFollowUp(
	ctx context.Context,
	tenantID, id int64,
	req CompleteFollowUpRequest,
	rule resultRule,
	occurredAt *time.Time,
	actorID *int64,
	actorName string,
) (*FollowUp, []*FollowUpEvent, *OutcomeEvent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("begin complete follow up: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	query := `SELECT ` + followUpColumns + ` FROM deal_follow_ups WHERE tenant_id = $1 AND id = $2 FOR UPDATE`
	current, err := scanFollowUp(tx.QueryRow(ctx, query, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, ErrFollowUpNotFound
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("lock follow up: %w", err)
	}
	if !IsActiveFollowUpStatus(current.Status) {
		return nil, nil, nil, ErrFollowUpNotActive
	}

	update := `UPDATE deal_follow_ups
		SET status = $3, result = $4, result_note = $5, next_action = $6,
		    closed_at = CASE WHEN $3 IN ('` + FollowUpCompleted + `','` + FollowUpCancelled + `') THEN NOW() ELSE NULL END,
		    updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
		RETURNING ` + followUpColumns
	updated, err := scanFollowUp(tx.QueryRow(ctx, update, tenantID, id, rule.nextStatus,
		req.Result, req.ResultNote, req.NextAction))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("update follow up: %w", err)
	}

	_, err = scanEvent(tx.QueryRow(ctx, `
		INSERT INTO deal_follow_up_events (tenant_id, follow_up_id, from_status, to_status, result, note, actor_employee_id, actor_name)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, follow_up_id, from_status, to_status, result, note, actor_employee_id, actor_name, created_at`,
		tenantID, id, current.Status, rule.nextStatus, req.Result, req.ResultNote, actorID, actorName))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("insert follow up event: %w", err)
	}

	var outcome *OutcomeEvent
	if rule.outcomeType != "" {
		if occurredAt == nil {
			now := time.Now()
			occurredAt = &now
		}
		outcome, err = scanOutcome(tx.QueryRow(ctx, `
			INSERT INTO deal_outcome_events (tenant_id, encounter_id, recording_id, type, amount, occurred_at, source, note, confirmed_by)
			VALUES ($1,$2,$3,$4,$5,$6,'employee',$7,$8)
			RETURNING `+outcomeColumns,
			tenantID, current.EncounterID, current.RecordingID, rule.outcomeType, req.Amount, occurredAt, req.ResultNote, actorID))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("insert outcome event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, nil, fmt.Errorf("commit complete follow up: %w", err)
	}

	events, err := s.ListFollowUpEvents(ctx, tenantID, id)
	if err != nil {
		return updated, nil, outcome, nil
	}
	return updated, events, outcome, nil
}

func scanEvent(row rowScanner) (*FollowUpEvent, error) {
	var item FollowUpEvent
	err := row.Scan(&item.ID, &item.FollowUpID, &item.FromStatus, &item.ToStatus, &item.Result,
		&item.Note, &item.ActorEmployeeID, &item.ActorName, &item.CreatedAt)
	return &item, err
}

func (s *Store) ListFollowUpEvents(ctx context.Context, tenantID, followUpID int64) ([]*FollowUpEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, follow_up_id, from_status, to_status, result, note, actor_employee_id, actor_name, created_at
		FROM deal_follow_up_events
		WHERE tenant_id = $1 AND follow_up_id = $2
		ORDER BY id ASC`, tenantID, followUpID)
	if err != nil {
		return nil, fmt.Errorf("list follow up events: %w", err)
	}
	defer rows.Close()

	items := make([]*FollowUpEvent, 0, 4)
	for rows.Next() {
		item, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan follow up event: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateOutcome(ctx context.Context, tenantID int64, req CreateOutcomeRequest, occurredAt *time.Time) (*OutcomeEvent, error) {
	if occurredAt == nil {
		now := time.Now()
		occurredAt = &now
	}
	item, err := scanOutcome(s.pool.QueryRow(ctx, `
		INSERT INTO deal_outcome_events (tenant_id, encounter_id, recording_id, type, amount, occurred_at, source, source_ref, note)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+outcomeColumns,
		tenantID, req.EncounterID, req.RecordingID, req.Type, req.Amount, occurredAt, req.Source, req.SourceRef, req.Note))
	if err != nil {
		return nil, fmt.Errorf("create outcome: %w", err)
	}
	return item, nil
}

func (s *Store) ListOutcomesByEncounter(ctx context.Context, tenantID, encounterID int64) ([]*OutcomeEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+outcomeColumns+`
		FROM deal_outcome_events
		WHERE tenant_id = $1 AND encounter_id = $2
		ORDER BY occurred_at ASC, id ASC`, tenantID, encounterID)
	if err != nil {
		return nil, fmt.Errorf("list outcomes: %w", err)
	}
	defer rows.Close()

	items := make([]*OutcomeEvent, 0, 4)
	for rows.Next() {
		item, err := scanOutcome(rows)
		if err != nil {
			return nil, fmt.Errorf("scan outcome: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
