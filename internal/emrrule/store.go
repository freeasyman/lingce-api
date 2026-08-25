package emrrule

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) LoadRecord(ctx context.Context, tenantID, recordID int64) (*RecordInput, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT r.id, r.tenant_id, r.encounter_id, r.customer_id, r.template_code, r.template_name, r.status,
		       r.chief_complaint, r.diagnosis_summary, r.patient_name, r.patient_gender, r.patient_age_text,
		       r.patient_phone, r.encountered_at, r.archived_at, r.updated_at,
		       r.patient_snapshot_json, r.document_json,
		       (SELECT MAX(version_no) FROM emr_document_versions v WHERE v.tenant_id = r.tenant_id AND v.document_id = r.id)
		FROM emr_outpatient_records r
		WHERE r.tenant_id = $1 AND r.id = $2
	`, tenantID, recordID)
	var record RecordInput
	var snapshotRaw, documentRaw []byte
	if err := row.Scan(
		&record.ID, &record.TenantID, &record.EncounterID, &record.CustomerID, &record.TemplateCode, &record.TemplateName, &record.Status,
		&record.ChiefComplaint, &record.DiagnosisSummary, &record.PatientName, &record.PatientGender, &record.PatientAgeText,
		&record.PatientPhone, &record.EncounteredAt, &record.ArchivedAt, &record.UpdatedAt,
		&snapshotRaw, &documentRaw, &record.LatestVersionNo,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load emr rule record: %w", err)
	}
	record.PatientSnapshot = JSONMap{}
	record.DocumentJSON = JSONMap{}
	_ = json.Unmarshal(snapshotRaw, &record.PatientSnapshot)
	_ = json.Unmarshal(documentRaw, &record.DocumentJSON)
	return &record, nil
}

func (s *Store) ListExecutableRules(ctx context.Context, tenantID int64) ([]RuleDefinition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, name, category, severity, trigger_type, conditions, description, legal_basis, suggested_script
		FROM compliance_rules
		WHERE deleted_at IS NULL
		  AND enabled = TRUE
		  AND scope = 'emr'
		  AND (tenant_id = 0 OR tenant_id = $1)
		  AND code = ANY($2)
		ORDER BY tenant_id ASC, code ASC
	`, tenantID, []string{
		"emr.patient.basic_info",
		"emr.time.consistency",
		"emr.chief_complaint.norm",
		"emr.past_history.complete",
		"emr.personal_history.complete",
		"emr.family_history.complete",
		"emr.present_illness.general_status",
		"emr.auxiliary_exam.closed_loop",
		"emr.allergy.required",
		"emr.allergy.prescription_risk",
		"emr.diagnosis.plan",
		"emr.treatment.consistency",
		"emr.tcm.western_diagnosis_prompt",
		"emr.followup.complete",
		"emr.signature.required",
		"emr.gynecology.menstrual_history",
		"emr.gynecology.marriage_childbearing",
		"emr.gynecology.exam_required",
		"emr.dental.tooth_plan",
		"emr.dental.informed_consent",
		"emr.pediatrics.guardian_info",
		"emr.pediatrics.fever_peak",
		"emr.pediatrics.weight_dose",
		"emr.pediatrics.feeding_growth",
	})
	if err != nil {
		return nil, fmt.Errorf("list executable emr rules: %w", err)
	}
	defer rows.Close()

	rules := make([]RuleDefinition, 0)
	for rows.Next() {
		var rule RuleDefinition
		var conditionsRaw []byte
		if err := rows.Scan(&rule.ID, &rule.Code, &rule.Name, &rule.Category, &rule.Severity, &rule.TriggerType, &conditionsRaw, &rule.Description, &rule.LegalBasis, &rule.SuggestedScript); err != nil {
			return nil, fmt.Errorf("scan emr rule: %w", err)
		}
		rule.Conditions = JSONMap{}
		_ = json.Unmarshal(conditionsRaw, &rule.Conditions)
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *Store) CreateRun(ctx context.Context, record *RecordInput, stage, triggerSource string, actorID int64) (int64, error) {
	snapshot, _ := json.Marshal(JSONMap{"record": record.DocumentJSON, "patient": record.PatientSnapshot})
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO emr_rule_runs (
			tenant_id, record_id, encounter_id, patient_id, template_code, template_name, stage, trigger_source,
			trigger_user_id, status, input_record_version, input_snapshot_json, started_at, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'running',$10,$11::jsonb,NOW(),NOW())
		RETURNING id
	`, record.TenantID, record.ID, record.EncounterID, record.CustomerID, record.TemplateCode, record.TemplateName, stage, triggerSource, actorID, record.LatestVersionNo, string(snapshot)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create emr rule run: %w", err)
	}
	return id, nil
}

func (s *Store) ReplaceRunHits(ctx context.Context, runID int64, record *RecordInput, stage string, hits []RuleHit) ([]RuleHit, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin emr rule hits tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE emr_rule_hits
		SET is_active = FALSE, updated_at = NOW()
		WHERE tenant_id = $1 AND record_id = $2 AND stage = $3 AND is_active = TRUE
	`, record.TenantID, record.ID, stage); err != nil {
		return nil, fmt.Errorf("deactivate previous emr rule hits: %w", err)
	}

	inserted := make([]RuleHit, 0, len(hits))
	for _, hit := range hits {
		policyRaw, _ := json.Marshal(hit.ActionPolicy)
		var hitID int64
		err := tx.QueryRow(ctx, `
			INSERT INTO emr_rule_hits (
				tenant_id, run_id, record_id, rule_id, rule_code, rule_version, rule_name_snapshot,
				stage, executor_type, target_type, target_path, hit_status, severity, action_policy_json,
				doctor_message, qc_message, summary, current_state, is_active, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,'v1',$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14,$15,$16,'open',TRUE,NOW(),NOW())
			RETURNING id
		`, record.TenantID, runID, record.ID, hit.RuleID, hit.RuleCode, hit.RuleName, stage, hit.ExecutorType, hit.TargetType, hit.TargetPath, hit.HitStatus, hit.Severity, string(policyRaw), hit.DoctorMessage, hit.QCMessage, hit.Summary).Scan(&hitID)
		if err != nil {
			return nil, fmt.Errorf("insert emr rule hit %s: %w", hit.RuleCode, err)
		}
		hit.ID = hitID
		hit.RunID = runID
		for i := range hit.Evidence {
			evidence := hit.Evidence[i]
			structuredRaw, _ := json.Marshal(evidence.StructuredValue)
			expectedRaw, _ := json.Marshal(evidence.ExpectedValue)
			actualRaw, _ := json.Marshal(evidence.ActualValue)
			var evidenceID int64
			if err := tx.QueryRow(ctx, `
				INSERT INTO emr_rule_evidences (
					tenant_id, hit_id, evidence_type, field_path, field_label, text_excerpt,
					structured_value_json, expected_value_json, actual_value_json, created_at
				) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9::jsonb,NOW())
				RETURNING id
			`, record.TenantID, hitID, evidence.EvidenceType, evidence.FieldPath, evidence.FieldLabel, evidence.TextExcerpt, string(structuredRaw), string(expectedRaw), string(actualRaw)).Scan(&evidenceID); err != nil {
				return nil, fmt.Errorf("insert emr rule evidence: %w", err)
			}
			hit.Evidence[i].ID = evidenceID
			hit.Evidence[i].HitID = hitID
		}
		inserted = append(inserted, hit)
	}

	status := "success"
	if _, err := tx.Exec(ctx, `UPDATE emr_rule_runs SET status = $2, finished_at = NOW() WHERE id = $1`, runID, status); err != nil {
		return nil, fmt.Errorf("finish emr rule run: %w", err)
	}
	if err := upsertSummary(ctx, tx, record, runID, stage); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit emr rule hits tx: %w", err)
	}
	return inserted, nil
}

func upsertSummary(ctx context.Context, tx pgx.Tx, record *RecordInput, runID int64, stage string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO emr_rule_summaries (
			tenant_id, record_id, record_version, blocking_count, important_count, notice_count,
			unhandled_count, acknowledged_count, resolved_count, can_submit, can_archive,
			last_run_id, last_run_stage, last_checked_at, updated_at
		)
		SELECT $1, $2, $3,
		       COUNT(*) FILTER (WHERE severity = 'blocking' AND is_active = TRUE AND current_state = 'open'),
		       COUNT(*) FILTER (WHERE severity = 'important' AND is_active = TRUE AND current_state = 'open'),
		       COUNT(*) FILTER (WHERE severity = 'notice' AND is_active = TRUE AND current_state = 'open'),
		       COUNT(*) FILTER (WHERE is_active = TRUE AND current_state = 'open'),
		       COUNT(*) FILTER (WHERE is_active = TRUE AND current_state IN ('acknowledged','not_applicable')),
		       COUNT(*) FILTER (WHERE is_active = TRUE AND current_state IN ('resolved','qc_approved')),
		       NOT EXISTS (SELECT 1 FROM emr_rule_hits WHERE tenant_id = $1 AND record_id = $2 AND is_active = TRUE AND current_state = 'open' AND severity = 'blocking' AND (action_policy_json->>'block_submit')::boolean IS TRUE),
		       NOT EXISTS (SELECT 1 FROM emr_rule_hits WHERE tenant_id = $1 AND record_id = $2 AND is_active = TRUE AND current_state = 'open' AND severity = 'blocking' AND (action_policy_json->>'block_archive')::boolean IS TRUE),
		       $4, $5, NOW(), NOW()
		FROM emr_rule_hits
		WHERE tenant_id = $1 AND record_id = $2 AND is_active = TRUE
		ON CONFLICT (tenant_id, record_id) DO UPDATE
		SET record_version = EXCLUDED.record_version,
		    blocking_count = EXCLUDED.blocking_count,
		    important_count = EXCLUDED.important_count,
		    notice_count = EXCLUDED.notice_count,
		    unhandled_count = EXCLUDED.unhandled_count,
		    acknowledged_count = EXCLUDED.acknowledged_count,
		    resolved_count = EXCLUDED.resolved_count,
		    can_submit = EXCLUDED.can_submit,
		    can_archive = EXCLUDED.can_archive,
		    last_run_id = EXCLUDED.last_run_id,
		    last_run_stage = EXCLUDED.last_run_stage,
		    last_checked_at = EXCLUDED.last_checked_at,
		    updated_at = NOW()
	`, record.TenantID, record.ID, record.LatestVersionNo, runID, stage)
	if err != nil {
		return fmt.Errorf("upsert emr rule summary: %w", err)
	}
	return nil
}

func (s *Store) GetSummary(ctx context.Context, tenantID, recordID int64) (*RuleSummary, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT record_id, record_version, blocking_count, important_count, notice_count, unhandled_count,
		       acknowledged_count, resolved_count, can_submit, can_archive, last_run_id,
		       last_run_stage, COALESCE(last_checked_at::text, '')
		FROM emr_rule_summaries
		WHERE tenant_id = $1 AND record_id = $2
	`, tenantID, recordID)
	var summary RuleSummary
	if err := row.Scan(&summary.RecordID, &summary.RecordVersion, &summary.BlockingCount, &summary.ImportantCount, &summary.NoticeCount, &summary.UnhandledCount, &summary.AcknowledgedCount, &summary.ResolvedCount, &summary.CanSubmit, &summary.CanArchive, &summary.LastRunID, &summary.LastRunStage, &summary.LastCheckedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get emr rule summary: %w", err)
	}
	return &summary, nil
}

func (s *Store) ListHits(ctx context.Context, tenantID, recordID int64, activeOnly bool) ([]RuleHit, error) {
	query := `
		SELECT id, run_id, record_id, rule_id, rule_code, rule_name_snapshot, stage, executor_type,
		       target_type, target_path, hit_status, severity, action_policy_json, doctor_message,
		       qc_message, summary, current_state, is_active, created_at::text, updated_at::text
		FROM emr_rule_hits
		WHERE tenant_id = $1 AND record_id = $2`
	if activeOnly {
		query += ` AND is_active = TRUE`
	}
	query += ` ORDER BY is_active DESC, created_at DESC, id DESC`
	rows, err := s.pool.Query(ctx, query, tenantID, recordID)
	if err != nil {
		return nil, fmt.Errorf("list emr rule hits: %w", err)
	}
	defer rows.Close()
	hits := make([]RuleHit, 0)
	for rows.Next() {
		var hit RuleHit
		var policyRaw []byte
		if err := rows.Scan(&hit.ID, &hit.RunID, &hit.RecordID, &hit.RuleID, &hit.RuleCode, &hit.RuleName, &hit.Stage, &hit.ExecutorType, &hit.TargetType, &hit.TargetPath, &hit.HitStatus, &hit.Severity, &policyRaw, &hit.DoctorMessage, &hit.QCMessage, &hit.Summary, &hit.CurrentState, &hit.IsActive, &hit.CreatedAt, &hit.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan emr rule hit: %w", err)
		}
		hit.ActionPolicy = JSONMap{}
		_ = json.Unmarshal(policyRaw, &hit.ActionPolicy)
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

func (s *Store) MarkDoctorAction(ctx context.Context, tenantID, recordID, hitID, actorID int64, actorRole, actionType, comment string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin doctor action tx: %w", err)
	}
	defer tx.Rollback(ctx)
	var state string
	switch actionType {
	case "locate":
		state = "located"
	case "mark_not_applicable":
		state = "not_applicable"
	case "acknowledge", "dismiss_notice":
		state = "acknowledged"
	case "edit":
		state = "edited"
	case "rerun":
		state = "open"
	default:
		return fmt.Errorf("invalid doctor action type")
	}
	if (actionType == "mark_not_applicable" || actionType == "acknowledge") && comment == "" {
		return fmt.Errorf("comment is required")
	}
	cmd, err := tx.Exec(ctx, `
		UPDATE emr_rule_hits
		SET current_state = $4, updated_at = NOW()
		WHERE tenant_id = $1 AND record_id = $2 AND id = $3 AND is_active = TRUE
	`, tenantID, recordID, hitID, state)
	if err != nil {
		return fmt.Errorf("update emr rule hit action state: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("rule hit not found")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO emr_rule_doctor_actions (tenant_id, hit_id, record_id, action_type, actor_user_id, actor_role, comment, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
	`, tenantID, hitID, recordID, actionType, actorID, actorRole, comment); err != nil {
		return fmt.Errorf("insert emr rule doctor action: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE emr_rule_summaries s SET
		  blocking_count = src.blocking_count,
		  important_count = src.important_count,
		  notice_count = src.notice_count,
		  unhandled_count = src.unhandled_count,
		  acknowledged_count = src.acknowledged_count,
		  resolved_count = src.resolved_count,
		  can_submit = src.can_submit,
		  can_archive = src.can_archive,
		  updated_at = NOW()
		FROM (
		  SELECT COUNT(*) FILTER (WHERE severity = 'blocking' AND is_active = TRUE AND current_state = 'open') AS blocking_count,
		         COUNT(*) FILTER (WHERE severity = 'important' AND is_active = TRUE AND current_state = 'open') AS important_count,
		         COUNT(*) FILTER (WHERE severity = 'notice' AND is_active = TRUE AND current_state = 'open') AS notice_count,
		         COUNT(*) FILTER (WHERE is_active = TRUE AND current_state = 'open') AS unhandled_count,
		         COUNT(*) FILTER (WHERE is_active = TRUE AND current_state IN ('acknowledged','not_applicable')) AS acknowledged_count,
		         COUNT(*) FILTER (WHERE is_active = TRUE AND current_state IN ('resolved','qc_approved')) AS resolved_count,
		         NOT EXISTS (SELECT 1 FROM emr_rule_hits WHERE tenant_id = $1 AND record_id = $2 AND is_active = TRUE AND current_state = 'open' AND severity = 'blocking' AND (action_policy_json->>'block_submit')::boolean IS TRUE) AS can_submit,
		         NOT EXISTS (SELECT 1 FROM emr_rule_hits WHERE tenant_id = $1 AND record_id = $2 AND is_active = TRUE AND current_state = 'open' AND severity = 'blocking' AND (action_policy_json->>'block_archive')::boolean IS TRUE) AS can_archive
		  FROM emr_rule_hits WHERE tenant_id = $1 AND record_id = $2
		) src
		WHERE s.tenant_id = $1 AND s.record_id = $2
	`, tenantID, recordID); err != nil {
		return fmt.Errorf("refresh emr rule summary: %w", err)
	}
	return tx.Commit(ctx)
}
