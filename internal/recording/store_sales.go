package recording

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ===== Lingce Sales Prospect Store =====

// CreateLingceSalesProspect inserts a new prospect record.
func (s *Store) CreateLingceSalesProspect(ctx context.Context, p *LingceSalesProspect) (int64, error) {
	query := `
		INSERT INTO lingce_sales_prospects (
			tenant_id, institution_name, institution_type, institution_scale, region,
			contact_name, contact_role, contact_phone, contact_wechat,
			decision_stage, deal_probability, source, assigned_to,
			next_follow_up_at, status, notes, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12, $13,
			$14, $15, $16, $17, $18
		) RETURNING id`

	now := time.Now()
	var id int64
	err := s.pool.QueryRow(ctx, query,
		p.TenantID, p.InstitutionName, p.InstitutionType, p.InstitutionScale, p.Region,
		p.ContactName, p.ContactRole, p.ContactPhone, p.ContactWechat,
		p.DecisionStage, p.DealProbability, p.Source, p.AssignedTo,
		p.NextFollowUpAt, p.Status, p.Notes, now, now,
	).Scan(&id)
	return id, err
}

// GetLingceSalesProspect retrieves a prospect by ID and tenant.
func (s *Store) GetLingceSalesProspect(ctx context.Context, tenantID, prospectID int64) (*LingceSalesProspect, error) {
	query := `
		SELECT id, tenant_id, institution_name, institution_type, institution_scale, region,
			contact_name, contact_role, contact_phone, contact_wechat,
			pain_points, decision_stage, deal_probability, budget_signal,
			competitor_mentions, decision_chain, internal_supporters, internal_blockers,
			source, assigned_to, next_action, next_follow_up_at,
			status, won_at, lost_reason, notes, created_at, updated_at
		FROM lingce_sales_prospects
		WHERE id = $1 AND tenant_id = $2`
	p := &LingceSalesProspect{}
	err := s.pool.QueryRow(ctx, query, prospectID, tenantID).Scan(
		&p.ID, &p.TenantID, &p.InstitutionName, &p.InstitutionType, &p.InstitutionScale, &p.Region,
		&p.ContactName, &p.ContactRole, &p.ContactPhone, &p.ContactWechat,
		&p.PainPoints, &p.DecisionStage, &p.DealProbability, &p.BudgetSignal,
		&p.CompetitorMentions, &p.DecisionChain, &p.InternalSupporters, &p.InternalBlockers,
		&p.Source, &p.AssignedTo, &p.NextAction, &p.NextFollowUpAt,
		&p.Status, &p.WonAt, &p.LostReason, &p.Notes, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ListLingceSalesProspects retrieves a paginated list of prospects with optional filters.
func (s *Store) ListLingceSalesProspects(ctx context.Context, req LingceSalesProspectListRequest) ([]*LingceSalesProspect, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
	args = append(args, req.TenantID)
	argIndex++

	if req.DecisionStage != nil {
		conditions = append(conditions, fmt.Sprintf("decision_stage = $%d", argIndex))
		args = append(args, *req.DecisionStage)
		argIndex++
	}
	if req.DealProbability != nil {
		conditions = append(conditions, fmt.Sprintf("deal_probability = $%d", argIndex))
		args = append(args, *req.DealProbability)
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

	where := strings.Join(conditions, " AND ")

	// Count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM lingce_sales_prospects WHERE %s", where)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Data
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	dataQuery := fmt.Sprintf(`
		SELECT id, tenant_id, institution_name, institution_type, institution_scale, region,
			contact_name, contact_role, contact_phone, contact_wechat,
			pain_points, decision_stage, deal_probability, budget_signal,
			competitor_mentions, decision_chain, internal_supporters, internal_blockers,
			source, assigned_to, next_action, next_follow_up_at,
			status, won_at, lost_reason, notes, created_at, updated_at
		FROM lingce_sales_prospects
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, argIndex, argIndex+1)
	args = append(args, pageSize, offset)

	rows, err := s.pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var prospects []*LingceSalesProspect
	for rows.Next() {
		p := &LingceSalesProspect{}
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.InstitutionName, &p.InstitutionType, &p.InstitutionScale, &p.Region,
			&p.ContactName, &p.ContactRole, &p.ContactPhone, &p.ContactWechat,
			&p.PainPoints, &p.DecisionStage, &p.DealProbability, &p.BudgetSignal,
			&p.CompetitorMentions, &p.DecisionChain, &p.InternalSupporters, &p.InternalBlockers,
			&p.Source, &p.AssignedTo, &p.NextAction, &p.NextFollowUpAt,
			&p.Status, &p.WonAt, &p.LostReason, &p.Notes, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		prospects = append(prospects, p)
	}
	return prospects, total, nil
}

// UpdateLingceSalesProspect updates a prospect with dynamic fields.
func (s *Store) UpdateLingceSalesProspect(ctx context.Context, tenantID, prospectID int64, req UpdateLingceSalesProspectRequest) error {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.InstitutionName != nil {
		setClauses = append(setClauses, fmt.Sprintf("institution_name = $%d", argIndex))
		args = append(args, *req.InstitutionName)
		argIndex++
	}
	if req.InstitutionType != nil {
		setClauses = append(setClauses, fmt.Sprintf("institution_type = $%d", argIndex))
		args = append(args, *req.InstitutionType)
		argIndex++
	}
	if req.InstitutionScale != nil {
		setClauses = append(setClauses, fmt.Sprintf("institution_scale = $%d", argIndex))
		args = append(args, *req.InstitutionScale)
		argIndex++
	}
	if req.Region != nil {
		setClauses = append(setClauses, fmt.Sprintf("region = $%d", argIndex))
		args = append(args, *req.Region)
		argIndex++
	}
	if req.ContactName != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_name = $%d", argIndex))
		args = append(args, *req.ContactName)
		argIndex++
	}
	if req.ContactRole != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_role = $%d", argIndex))
		args = append(args, *req.ContactRole)
		argIndex++
	}
	if req.ContactPhone != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_phone = $%d", argIndex))
		args = append(args, *req.ContactPhone)
		argIndex++
	}
	if req.ContactWechat != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_wechat = $%d", argIndex))
		args = append(args, *req.ContactWechat)
		argIndex++
	}
	if req.DecisionStage != nil {
		setClauses = append(setClauses, fmt.Sprintf("decision_stage = $%d", argIndex))
		args = append(args, *req.DecisionStage)
		argIndex++
	}
	if req.DealProbability != nil {
		setClauses = append(setClauses, fmt.Sprintf("deal_probability = $%d", argIndex))
		args = append(args, *req.DealProbability)
		argIndex++
	}
	if req.BudgetSignal != nil {
		setClauses = append(setClauses, fmt.Sprintf("budget_signal = $%d", argIndex))
		args = append(args, *req.BudgetSignal)
		argIndex++
	}
	if req.InternalSupporters != nil {
		setClauses = append(setClauses, fmt.Sprintf("internal_supporters = $%d", argIndex))
		args = append(args, *req.InternalSupporters)
		argIndex++
	}
	if req.InternalBlockers != nil {
		setClauses = append(setClauses, fmt.Sprintf("internal_blockers = $%d", argIndex))
		args = append(args, *req.InternalBlockers)
		argIndex++
	}
	if req.AssignedTo != nil {
		setClauses = append(setClauses, fmt.Sprintf("assigned_to = $%d", argIndex))
		args = append(args, *req.AssignedTo)
		argIndex++
	}
	if req.NextAction != nil {
		setClauses = append(setClauses, fmt.Sprintf("next_action = $%d", argIndex))
		args = append(args, *req.NextAction)
		argIndex++
	}
	if req.NextFollowUpAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("next_follow_up_at = $%d", argIndex))
		args = append(args, *req.NextFollowUpAt)
		argIndex++
	}
	if req.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}
	if req.LostReason != nil {
		setClauses = append(setClauses, fmt.Sprintf("lost_reason = $%d", argIndex))
		args = append(args, *req.LostReason)
		argIndex++
	}
	if req.Notes != nil {
		setClauses = append(setClauses, fmt.Sprintf("notes = $%d", argIndex))
		args = append(args, *req.Notes)
		argIndex++
	}

	if len(setClauses) == 0 {
		return nil // nothing to update
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIndex))
	args = append(args, time.Now())
	argIndex++

	args = append(args, prospectID, tenantID)
	query := fmt.Sprintf("UPDATE lingce_sales_prospects SET %s WHERE id = $%d AND tenant_id = $%d",
		strings.Join(setClauses, ", "), argIndex, argIndex+1)

	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

// UpdateLingceSalesProspectPainPoints updates the JSONB pain_points field.
func (s *Store) UpdateLingceSalesProspectPainPoints(ctx context.Context, tenantID, prospectID int64, painPoints JSONObject) error {
	query := `UPDATE lingce_sales_prospects SET pain_points = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`
	_, err := s.pool.Exec(ctx, query, painPoints, time.Now(), prospectID, tenantID)
	return err
}

// UpdateLingceSalesProspectDecisionChain updates the JSONB decision_chain field.
func (s *Store) UpdateLingceSalesProspectDecisionChain(ctx context.Context, tenantID, prospectID int64, chain JSONObject) error {
	query := `UPDATE lingce_sales_prospects SET decision_chain = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`
	_, err := s.pool.Exec(ctx, query, chain, time.Now(), prospectID, tenantID)
	return err
}

// UpdateLingceSalesProspectCompetitorMentions updates the JSONB competitor_mentions field.
func (s *Store) UpdateLingceSalesProspectCompetitorMentions(ctx context.Context, tenantID, prospectID int64, mentions JSONObject) error {
	query := `UPDATE lingce_sales_prospects SET competitor_mentions = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`
	_, err := s.pool.Exec(ctx, query, mentions, time.Now(), prospectID, tenantID)
	return err
}

// ===== Prospect-Recording Association =====

// AssociateRecordingToProspect creates an association between a recording and a prospect.
func (s *Store) AssociateRecordingToProspect(ctx context.Context, rec *LingceSalesProspectRecording) (int64, error) {
	query := `
		INSERT INTO lingce_sales_prospect_recordings (
			prospect_id, recording_id, conversation_type, conversation_purpose, created_at
		) VALUES ($1, $2, $3, $4, $5)
		RETURNING id`
	var id int64
	err := s.pool.QueryRow(ctx, query,
		rec.ProspectID, rec.RecordingID, rec.ConversationType, rec.ConversationPurpose, time.Now(),
	).Scan(&id)
	return id, err
}

// ListProspectRecordings retrieves recordings associated with a prospect.
func (s *Store) ListProspectRecordings(ctx context.Context, prospectID int64) ([]*LingceSalesProspectRecording, error) {
	query := `
		SELECT id, prospect_id, recording_id, conversation_type, conversation_purpose, created_at
		FROM lingce_sales_prospect_recordings
		WHERE prospect_id = $1
		ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, query, prospectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []*LingceSalesProspectRecording
	for rows.Next() {
		r := &LingceSalesProspectRecording{}
		if err := rows.Scan(&r.ID, &r.ProspectID, &r.RecordingID, &r.ConversationType, &r.ConversationPurpose, &r.CreatedAt); err != nil {
			return nil, err
		}
		recs = append(recs, r)
	}
	return recs, nil
}

// ===== Stage Change History =====

// CreateStageChange inserts a stage change record.
func (s *Store) CreateStageChange(ctx context.Context, sc *LingceSalesProspectStageChange) (int64, error) {
	query := `
		INSERT INTO lingce_sales_prospect_stage_changes (
			prospect_id, recording_id, from_stage, to_stage, change_type, reason, changed_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`
	var id int64
	err := s.pool.QueryRow(ctx, query,
		sc.ProspectID, sc.RecordingID, sc.FromStage, sc.ToStage, sc.ChangeType, sc.Reason, sc.ChangedBy, time.Now(),
	).Scan(&id)
	return id, err
}

// ListStageChanges retrieves stage change history for a prospect.
func (s *Store) ListStageChanges(ctx context.Context, prospectID int64) ([]*LingceSalesProspectStageChange, error) {
	query := `
		SELECT id, prospect_id, recording_id, from_stage, to_stage, change_type, reason, changed_by, created_at
		FROM lingce_sales_prospect_stage_changes
		WHERE prospect_id = $1
		ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, query, prospectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changes []*LingceSalesProspectStageChange
	for rows.Next() {
		sc := &LingceSalesProspectStageChange{}
		if err := rows.Scan(
			&sc.ID, &sc.ProspectID, &sc.RecordingID, &sc.FromStage, &sc.ToStage,
			&sc.ChangeType, &sc.Reason, &sc.ChangedBy, &sc.CreatedAt,
		); err != nil {
			return nil, err
		}
		changes = append(changes, sc)
	}
	return changes, nil
}

// UpdateProspectStage atomically updates the decision_stage of a prospect.
func (s *Store) UpdateProspectStage(ctx context.Context, tenantID, prospectID int64, newStage DecisionStage) error {
	query := `UPDATE lingce_sales_prospects SET decision_stage = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`
	_, err := s.pool.Exec(ctx, query, newStage, time.Now(), prospectID, tenantID)
	return err
}
