package emrcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) CurrentSnapshotID(ctx context.Context, tenantID int64, recordID string) (string, error) {
	var id *string
	err := s.pool.QueryRow(ctx, `
		SELECT current_snapshot_id::text
		FROM emr_records
		WHERE id=$1 AND tenant_id=$2
	`, recordID, tenantID).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("record not found")
	}
	if err != nil {
		return "", fmt.Errorf("get current emr snapshot: %w", err)
	}
	if id == nil {
		return "", nil
	}
	return *id, nil
}

func (s *Store) Run(ctx context.Context, req CheckRequest) (*CheckRun, error) {
	data, bindings, err := s.loadContext(ctx, req)
	if err != nil {
		return nil, err
	}
	if !validTrigger(req.TriggerAction) {
		return nil, fmt.Errorf("invalid check trigger action")
	}

	run := &CheckRun{}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO emr_check_runs
		(tenant_id, record_id, snapshot_id, template_version_id, trigger_action, status, overall_result, started_by)
		VALUES ($1,$2,$3,$4,$5,'执行中','无法完成',$6)
		RETURNING id, tenant_id, record_id, snapshot_id, template_version_id, trigger_action,
		 status, overall_result, started_at, completed_at, started_by, failure_reason
	`, req.TenantID, req.RecordID, req.SnapshotID, data.TemplateVersionID, req.TriggerAction, req.StartedBy).Scan(
		&run.ID, &run.TenantID, &run.RecordID, &run.SnapshotID, &run.TemplateVersionID,
		&run.TriggerAction, &run.Status, &run.OverallResult, &run.StartedAt, &run.CompletedAt,
		&run.StartedBy, &run.FailureReason)
	if err != nil {
		return nil, fmt.Errorf("create emr check run: %w", err)
	}

	results := make([]evaluatedResult, 0, len(bindings))
	for _, item := range bindings {
		results = append(results, evaluateRequirement(item, data, req.TriggerAction))
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return s.failRun(ctx, run.ID, fmt.Errorf("begin check result transaction: %w", err))
	}
	batch := &pgx.Batch{}
	for index, item := range bindings {
		result := results[index]
		evidence, marshalErr := json.Marshal(result.Evidence)
		if marshalErr != nil {
			_ = tx.Rollback(ctx)
			return s.failRun(ctx, run.ID, fmt.Errorf("marshal check evidence: %w", marshalErr))
		}
		batch.Queue(`
			INSERT INTO emr_check_results
			(check_run_id, quality_requirement_id, requirement_code, requirement_name,
			 configured_execution_mode, actual_execution_mode, conclusion, check_status,
			 incomplete_reason, handling_result, meets_deadline, evidence,
			 hit_explanation, suggested_handling)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13,$14)
		`, run.ID, item.QualityRequirementID, item.Code, item.Name, item.ExecutionMode,
			result.ActualMode, result.Conclusion, result.CheckStatus, result.IncompleteReason,
			result.HandlingResult, result.MeetsDeadline, string(evidence), result.Explanation,
			result.Suggested)
	}
	batchResults := tx.SendBatch(ctx, batch)
	for range bindings {
		if _, execErr := batchResults.Exec(); execErr != nil {
			_ = batchResults.Close()
			_ = tx.Rollback(ctx)
			return s.failRun(ctx, run.ID, fmt.Errorf("insert emr check result: %w", execErr))
		}
	}
	if err := batchResults.Close(); err != nil {
		_ = tx.Rollback(ctx)
		return s.failRun(ctx, run.ID, fmt.Errorf("close check result batch: %w", err))
	}
	overall := overallResult(results)
	completedAt := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE emr_check_runs
		SET status='已完成', overall_result=$2, completed_at=$3
		WHERE id=$1
	`, run.ID, overall, completedAt); err != nil {
		_ = tx.Rollback(ctx)
		return s.failRun(ctx, run.ID, fmt.Errorf("complete emr check run: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return s.failRun(ctx, run.ID, fmt.Errorf("commit emr check run: %w", err))
	}
	run.Status = "已完成"
	run.OverallResult = overall
	run.CompletedAt = &completedAt
	run.Results, err = s.listResults(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Store) loadContext(ctx context.Context, req CheckRequest) (checkContext, []binding, error) {
	data := checkContext{RecordID: req.RecordID, SnapshotID: req.SnapshotID, Content: map[string]any{}}
	var rawContent []byte
	err := s.pool.QueryRow(ctx, `
		SELECT r.template_version_id, r.visit_type, COALESCE(r.specialty_module,''),
		       COALESCE(snapshot.content,'{}'::jsonb), r.confirmed_by
		FROM emr_records r
		JOIN emr_record_snapshots snapshot ON snapshot.id=$3 AND snapshot.record_id=r.id
		WHERE r.id=$2 AND r.tenant_id=$1
	`, req.TenantID, req.RecordID, req.SnapshotID).Scan(
		&data.TemplateVersionID, &data.VisitType, &data.Specialty, &rawContent,
		&data.ConfirmedBy)
	if err != nil {
		return data, nil, fmt.Errorf("load emr check context: %w", err)
	}
	_ = json.Unmarshal(rawContent, &data.Content)
	bindingRows, err := s.pool.Query(ctx, `
		SELECT b.quality_requirement_id, q.code, q.name, q.rule_type,
		       b.execution_mode, b.deadline_action
		FROM emr_template_quality_requirements b
		JOIN emr_quality_requirements q ON q.id=b.quality_requirement_id
		WHERE b.template_version_id=$1 AND q.status='published'
		ORDER BY b.display_order, q.code
	`, data.TemplateVersionID)
	if err != nil {
		return data, nil, fmt.Errorf("load emr quality bindings for check: %w", err)
	}
	defer bindingRows.Close()
	bindings := make([]binding, 0)
	for bindingRows.Next() {
		item := binding{}
		if scanErr := bindingRows.Scan(&item.QualityRequirementID, &item.Code, &item.Name, &item.RuleType, &item.ExecutionMode, &item.DeadlineAction); scanErr != nil {
			return data, nil, scanErr
		}
		bindings = append(bindings, item)
	}
	return data, bindings, bindingRows.Err()
}

func (s *Store) failRun(ctx context.Context, id string, cause error) (*CheckRun, error) {
	reason := strings.TrimSpace(cause.Error())
	_, updateErr := s.pool.Exec(ctx, `UPDATE emr_check_runs SET status='执行失败', overall_result='无法完成', completed_at=NOW(), failure_reason=$2 WHERE id=$1`, id, reason)
	if updateErr != nil {
		return nil, fmt.Errorf("%v; mark check run failed: %w", cause, updateErr)
	}
	return nil, cause
}

func (s *Store) ListRuns(ctx context.Context, tenantID int64, recordID string) ([]*CheckRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, record_id, snapshot_id, template_version_id, trigger_action,
		       status, overall_result, started_at, completed_at, started_by, failure_reason
		FROM emr_check_runs WHERE tenant_id=$1 AND record_id=$2 ORDER BY started_at DESC, id DESC
	`, tenantID, recordID)
	if err != nil {
		return nil, fmt.Errorf("list emr check runs: %w", err)
	}
	defer rows.Close()
	items := make([]*CheckRun, 0)
	for rows.Next() {
		item := &CheckRun{}
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RecordID, &item.SnapshotID, &item.TemplateVersionID, &item.TriggerAction, &item.Status, &item.OverallResult, &item.StartedAt, &item.CompletedAt, &item.StartedBy, &item.FailureReason); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetRun(ctx context.Context, tenantID int64, recordID, id string) (*CheckRun, error) {
	item := &CheckRun{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, record_id, snapshot_id, template_version_id, trigger_action,
		       status, overall_result, started_at, completed_at, started_by, failure_reason
		FROM emr_check_runs WHERE id=$1 AND tenant_id=$2 AND record_id=$3
	`, id, tenantID, recordID).Scan(&item.ID, &item.TenantID, &item.RecordID, &item.SnapshotID, &item.TemplateVersionID, &item.TriggerAction, &item.Status, &item.OverallResult, &item.StartedAt, &item.CompletedAt, &item.StartedBy, &item.FailureReason)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get emr check run: %w", err)
	}
	item.Results, err = s.listResults(ctx, id)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Store) listResults(ctx context.Context, runID string) ([]*CheckResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, check_run_id, quality_requirement_id, requirement_code, requirement_name,
		       configured_execution_mode, actual_execution_mode, conclusion, check_status,
		       incomplete_reason, handling_result, meets_deadline, evidence,
		       hit_explanation, suggested_handling, manual_conclusion, manual_by, manual_at
		FROM emr_check_results WHERE check_run_id=$1 ORDER BY requirement_code
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list emr check results: %w", err)
	}
	defer rows.Close()
	items := make([]*CheckResult, 0)
	for rows.Next() {
		item := &CheckResult{}
		var rawEvidence []byte
		if err := rows.Scan(&item.ID, &item.CheckRunID, &item.QualityRequirementID, &item.RequirementCode, &item.RequirementName, &item.ConfiguredExecutionMode, &item.ActualExecutionMode, &item.Conclusion, &item.CheckStatus, &item.IncompleteReason, &item.HandlingResult, &item.MeetsDeadline, &rawEvidence, &item.HitExplanation, &item.SuggestedHandling, &item.ManualConclusion, &item.ManualBy, &item.ManualAt); err != nil {
			return nil, err
		}
		item.Evidence = map[string]any{}
		_ = json.Unmarshal(rawEvidence, &item.Evidence)
		items = append(items, item)
	}
	return items, rows.Err()
}

func validTrigger(value string) bool {
	switch value {
	case "手动保存", "提交", "确认", "归档", "人工重新检查":
		return true
	default:
		return false
	}
}

func overallResult(results []evaluatedResult) string {
	for _, result := range results {
		if result.HandlingResult == "阻断" {
			return "需要处理"
		}
	}
	for _, result := range results {
		if result.CheckStatus != "已完成" {
			return "无法完成"
		}
		if result.Conclusion == "不符合" {
			return "存在提示"
		}
	}
	return "无问题"
}
