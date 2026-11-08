package dealexpert

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type OutcomeFilter struct {
	DateFrom      *time.Time
	DateTo        *time.Time
	EmployeeID    *int64
	Type          string
	IncludeVoided bool
	Page          int
	PageSize      int
}

type OutcomePatch struct {
	Type       string   `json:"type"`
	Amount     *float64 `json:"amount"`
	OccurredAt *string  `json:"occurred_at"`
	Note       *string  `json:"note"`
	Reason     string   `json:"reason"`
}

func (s *Store) ListOutcomes(ctx context.Context, tenantID int64, f OutcomeFilter) ([]*OutcomeListItem, int, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 1000 {
		f.PageSize = 50
	}
	where := `o.tenant_id = $1`
	args := []any{tenantID}
	add := func(cond string, value any) {
		args = append(args, value)
		where += fmt.Sprintf(" AND "+cond, len(args))
	}
	if f.DateFrom != nil {
		add("o.occurred_at >= $%d", *f.DateFrom)
	}
	if f.DateTo != nil {
		add("o.occurred_at < $%d", *f.DateTo)
	}
	if f.EmployeeID != nil {
		add("r.employee_id = $%d", *f.EmployeeID)
	}
	if f.Type != "" {
		add("o.type = $%d", f.Type)
	}
	if !f.IncludeVoided {
		where += " AND o.voided_at IS NULL"
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM deal_outcome_events o JOIN recordings r ON r.id = o.recording_id WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count outcomes: %w", err)
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := s.pool.Query(ctx, `
		SELECT o.id, o.tenant_id, o.encounter_id, o.recording_id, o.type, o.amount, o.occurred_at,
		       o.source, o.source_ref, o.note, o.confirmed_by, o.created_at,
		       o.voided_at, o.void_reason, o.updated_by, o.update_reason, o.updated_at,
		       COALESCE(cu.name,''), COALESCE(NULLIF(e.full_name,''), e.name,''), COALESCE(NULLIF(c.card->'intent_project'->>'name',''), NULLIF(r.analysis_result->>'business_title',''), '')
		FROM deal_outcome_events o
		JOIN recordings r ON r.id = o.recording_id
		LEFT JOIN customers cu ON cu.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN deal_cards_v2 c ON c.recording_id = o.recording_id AND c.tenant_id = o.tenant_id AND c.status = 'analyzed'
		WHERE `+where+`
		ORDER BY o.occurred_at DESC, o.id DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list outcomes: %w", err)
	}
	defer rows.Close()
	items := make([]*OutcomeListItem, 0, f.PageSize)
	for rows.Next() {
		var it OutcomeListItem
		if err := rows.Scan(&it.ID, &it.TenantID, &it.EncounterID, &it.RecordingID, &it.Type, &it.Amount, &it.OccurredAt,
			&it.Source, &it.SourceRef, &it.Note, &it.ConfirmedBy, &it.CreatedAt,
			&it.VoidedAt, &it.VoidReason, &it.UpdatedBy, &it.UpdateReason, &it.UpdatedAt,
			&it.CustomerName, &it.ConsultantName, &it.ProductName); err != nil {
			return nil, 0, fmt.Errorf("scan outcome: %w", err)
		}
		items = append(items, &it)
	}
	return items, total, rows.Err()
}

func (s *Store) ListUnreportedClaims(ctx context.Context, tenantID int64, employeeID *int64) ([]*UnreportedClaim, error) {
	args := []any{tenantID}
	extra := ""
	if employeeID != nil {
		args = append(args, *employeeID)
		extra = fmt.Sprintf(" AND r.employee_id = $%d", len(args))
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.recording_id, COALESCE(r.encounter_id,0),
		       COALESCE(cu.name,''), COALESCE(NULLIF(e.full_name,''), e.name,''),
		       COALESCE(c.card->>'deal_status',''), COALESCE(c.card->>'stuck_point_headline',''),
		       r.recorded_at, COALESCE(c.card->'amount_hints','[]'::jsonb),
		       COALESCE(NULLIF(c.card->'intent_project'->>'name',''), NULLIF(r.analysis_result->>'business_title',''), '')
		FROM deal_cards_v2 c
		JOIN recordings r ON r.id = c.recording_id
		LEFT JOIN customers cu ON cu.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		WHERE c.tenant_id = $1 AND c.status = 'analyzed' AND r.deleted_at IS NULL
		  AND c.card->>'deal_status' = ANY(ARRAY['已预约','付款待治疗','已成交','疗程进行中','已经选择我们'])
		  AND NOT EXISTS (
		    SELECT 1 FROM deal_outcome_events o
		    WHERE o.tenant_id = c.tenant_id AND o.voided_at IS NULL
		      AND (o.recording_id = c.recording_id OR (r.encounter_id > 0 AND o.encounter_id = r.encounter_id))
		  )`+extra+`
		ORDER BY r.recorded_at DESC
		LIMIT 500`, args...)
	if err != nil {
		return nil, fmt.Errorf("list unreported: %w", err)
	}
	defer rows.Close()
	items := make([]*UnreportedClaim, 0, 32)
	for rows.Next() {
		var it UnreportedClaim
		var hintsRaw []byte
		var recorded time.Time
		var product string
		if err := rows.Scan(&it.RecordingID, &it.EncounterID, &it.CustomerName, &it.ConsultantName,
			&it.DealStatus, &it.StuckPoint, &recorded, &hintsRaw, &product); err != nil {
			return nil, fmt.Errorf("scan unreported: %w", err)
		}
		it.RecordedAt = recorded.Format(time.RFC3339)
		it.AmountHints = decodeAmountHints(hintsRaw)
		it.Suggested = maxAmountHint(hintsRaw)
		items = append(items, &it)
	}
	return items, rows.Err()
}

func (s *Store) UpdateOutcome(ctx context.Context, tenantID, id, actorID int64, patch OutcomePatch, occurredAt *time.Time) (*OutcomeEvent, error) {
	args := []any{tenantID, id, actorID, patch.Reason}
	sets := []string{"updated_by = $3", "update_reason = $4", "updated_at = now()"}
	idx := 5
	if patch.Type != "" {
		set, err := s.addArg(&args, &idx, "type", patch.Type)
		if err != nil {
			return nil, err
		}
		sets = append(sets, set)
	}
	if patch.Amount != nil {
		set, _ := s.addArg(&args, &idx, "amount", *patch.Amount)
		sets = append(sets, set)
	}
	if occurredAt != nil {
		set, _ := s.addArg(&args, &idx, "occurred_at", *occurredAt)
		sets = append(sets, set)
	}
	if patch.Note != nil {
		set, _ := s.addArg(&args, &idx, "note", *patch.Note)
		sets = append(sets, set)
	}
	row := s.pool.QueryRow(ctx, `UPDATE deal_outcome_events SET `+joinStrings(sets, ", ")+`
		WHERE tenant_id = $1 AND id = $2 AND voided_at IS NULL
		RETURNING `+outcomeColumns, args...)
	return scanOutcome(row)
}

func (s *Store) addArg(args *[]any, idx *int, col string, value any) (string, error) {
	*args = append(*args, value)
	set := fmt.Sprintf("%s = $%d", col, *idx)
	*idx++
	return set, nil
}

func joinStrings(values []string, sep string) string {
	out := ""
	for i, v := range values {
		if i > 0 {
			out += sep
		}
		out += v
	}
	return out
}

func (s *Store) VoidOutcome(ctx context.Context, tenantID, id, actorID int64, reason string) (*OutcomeEvent, error) {
	row := s.pool.QueryRow(ctx, `UPDATE deal_outcome_events
		SET voided_at = now(), void_reason = $3, updated_by = $4, update_reason = $3, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND voided_at IS NULL
		RETURNING `+outcomeColumns, tenantID, id, reason, actorID)
	return scanOutcome(row)
}

func decodeAmountHints(raw []byte) any {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return []any{}
	}
	return value
}

func maxAmountHint(raw []byte) *float64 {
	var hints []struct {
		Amount float64 `json:"amount"`
	}
	if err := json.Unmarshal(raw, &hints); err != nil {
		return nil
	}
	var max float64
	for _, hint := range hints {
		if hint.Amount > max {
			max = hint.Amount
		}
	}
	if max <= 0 {
		return nil
	}
	return &max
}
