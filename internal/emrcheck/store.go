package emrcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type previewData struct {
	TemplateCode          string
	TemplateName          string
	TemplateVersion       string
	TemplateVersionID     string
	MedicalDepartmentCode string
	Template              map[string]any
	TemplateFields        []map[string]any
	PatientContext        map[string]any
	EncounterContext      map[string]any
}

type qualityPrompt struct {
	Code         string
	Version      string
	SystemPrompt string
	UserPrompt   string
}

type qualityModelConfig struct {
	FunctionType string
	Provider     string
	ModelCode    string
	Params       *llmgateway.Params
}

func (s *Store) loadPreviewData(ctx context.Context, tenantID int64, versionID string) (*previewData, []previewBinding, error) {
	data := &previewData{TemplateVersionID: versionID}
	var documentType, visitType, status string
	var tenantScope int64
	var isCurrent bool
	err := s.pool.QueryRow(ctx, `
		SELECT code, name, version_no, COALESCE(specialty, ''), document_type, visit_type,
		       tenant_id, status, is_current
		FROM emr_templates
		WHERE id=$1 AND tenant_id IN (0,$2)
	`, versionID, tenantID).Scan(
		&data.TemplateCode, &data.TemplateName, &data.TemplateVersion, &data.MedicalDepartmentCode,
		&documentType, &visitType, &tenantScope, &status, &isCurrent)
	if err != nil {
		return nil, nil, fmt.Errorf("load emr template for quality check: %w", err)
	}
	data.Template = map[string]any{
		"code": data.TemplateCode, "name": data.TemplateName,
		"version": data.TemplateVersion, "version_id": data.TemplateVersionID,
		"medical_department_code": data.MedicalDepartmentCode, "document_type": documentType,
		"visit_type": visitType, "tenant_id": tenantScope, "status": status, "is_current": isCurrent,
	}
	rows, err := s.pool.Query(ctx, `
		SELECT tf.display_order, f.code, f.name, f.content_category, f.input_type,
		       tf.is_visible, tf.is_required, COALESCE(tf.description, f.description)
		FROM emr_template_fields tf
		JOIN emr_fields f ON f.id=tf.field_id
		WHERE tf.template_id=$1
		ORDER BY tf.display_order, f.code
	`, versionID)
	if err != nil {
		return nil, nil, fmt.Errorf("load emr template fields for quality check: %w", err)
	}
	defer rows.Close()
	data.TemplateFields = make([]map[string]any, 0)
	for rows.Next() {
		var order int
		var code, name, category, inputType, description string
		var visible, required bool
		if err := rows.Scan(&order, &code, &name, &category, &inputType, &visible, &required, &description); err != nil {
			return nil, nil, fmt.Errorf("scan emr template field: %w", err)
		}
		data.TemplateFields = append(data.TemplateFields, map[string]any{
			"display_order": order, "code": code, "name": name, "content_category": category,
			"input_type": inputType, "is_visible": visible, "is_required": required, "description": description,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	bindings, err := s.loadPreviewBindings(ctx, tenantID, versionID, true)
	if err != nil {
		return nil, nil, err
	}
	return data, bindings, nil
}

type previewBinding struct {
	binding
	rule qualityRuleInput
}

func (s *Store) loadPreviewBindings(ctx context.Context, tenantID int64, versionID string, withDetails bool) ([]previewBinding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.quality_rule_id, q.code, q.name, q.rule_type, b.execution_mode,
		       b.deadline_action, q.quality_group, q.evaluated_fact, q.pass_condition,
		       q.precondition, q.evidence_basis, q.source_version
		FROM emr_template_quality_rules b
		JOIN emr_quality_rules q ON q.id=b.quality_rule_id
		WHERE b.template_id=$1 AND b.is_enabled=true
		  AND q.status='published' AND q.tenant_id IN (0,$2)
		ORDER BY b.display_order, q.code
	`, versionID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load emr quality bindings: %w", err)
	}
	defer rows.Close()
	items := make([]previewBinding, 0)
	for rows.Next() {
		var item previewBinding
		if err := rows.Scan(&item.QualityRequirementID, &item.Code, &item.Name, &item.RuleType,
			&item.ExecutionMode, &item.DeadlineAction, &item.QualityGroup, &item.EvaluatedFact,
			&item.PassCondition, &item.Precondition, &item.EvidenceBasis, &item.SourceVersion); err != nil {
			return nil, err
		}
		item.rule = qualityRuleInput{
			QualityRequirementID: item.QualityRequirementID, Code: item.Code, Name: item.Name,
			RuleType: item.RuleType, QualityGroup: item.QualityGroup, EvaluatedFact: item.EvaluatedFact,
			PassCondition: item.PassCondition, Precondition: item.Precondition, EvidenceBasis: item.EvidenceBasis,
			SourceVersion: item.SourceVersion, ExecutionMode: item.ExecutionMode, DeadlineAction: item.DeadlineAction,
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) loadQualityPrompt(ctx context.Context) (*qualityPrompt, error) {
	item := &qualityPrompt{}
	err := s.pool.QueryRow(ctx, `
		SELECT code, COALESCE(version,'v1'), COALESCE(system_prompt,''), COALESCE(user_prompt_template,'')
		FROM recording_analysis_prompts
		WHERE code='emr_quality_check_v1' AND is_active=true
		ORDER BY updated_at DESC, id DESC LIMIT 1
	`).Scan(&item.Code, &item.Version, &item.SystemPrompt, &item.UserPrompt)
	if err != nil {
		return nil, fmt.Errorf("load emr quality prompt: %w", err)
	}
	if strings.TrimSpace(item.SystemPrompt) == "" || strings.TrimSpace(item.UserPrompt) == "" {
		return nil, fmt.Errorf("emr quality prompt is incomplete")
	}
	return item, nil
}

func (s *Store) loadQualityModelConfig(ctx context.Context, tenantID int64) (*qualityModelConfig, error) {
	item := &qualityModelConfig{}
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(function_type,''), COALESCE(provider,''), COALESCE(model_code,''),
		       COALESCE(model_params, extra_params, '{}'::json)
		FROM llm_model_configs
		WHERE deleted_at IS NULL AND COALESCE(is_active,true)=true
		  AND function_type='emr_quality_check' AND tenant_id IN ($1,0)
		ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END,
		         CASE WHEN COALESCE(is_default,false) THEN 0 ELSE 1 END,
		         updated_at DESC, id DESC LIMIT 1
	`, tenantID).Scan(&item.FunctionType, &item.Provider, &item.ModelCode, &raw)
	if err != nil {
		return nil, fmt.Errorf("load emr quality model config: %w", err)
	}
	params := &llmgateway.Params{Temperature: 0, MaxTokens: 8192, TimeoutSeconds: 120, ResponseFormat: "json"}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err == nil {
		if value, ok := values["temperature"].(float64); ok {
			params.Temperature = value
		}
		if value, ok := values["max_tokens"].(float64); ok && value > 0 {
			params.MaxTokens = int(value)
			if params.MaxTokens > 8192 {
				params.MaxTokens = 8192
			}
		}
		if value, ok := values["timeout_seconds"].(float64); ok && value > 0 {
			params.TimeoutSeconds = int(value)
		}
		if value, ok := values["response_format"].(string); ok && value != "" {
			params.ResponseFormat = value
		}
	}
	if item.FunctionType == "" || item.Provider == "" || item.ModelCode == "" {
		return nil, fmt.Errorf("emr quality model config is incomplete")
	}
	item.Params = params
	return item, nil
}

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
			 execution_status, incomplete_reason, handling_result, meets_deadline, evidence,
			 hit_explanation, suggested_handling)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14,$15)
		`, run.ID, item.QualityRequirementID, item.Code, item.Name, item.ExecutionMode,
			result.ActualMode, result.Conclusion, result.CheckStatus, result.ExecutionStatus, result.IncompleteReason,
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
		SELECT r.template_id, r.visit_type, COALESCE(t.specialty,''),
		       COALESCE(snapshot.content,'{}'::jsonb), r.confirmed_by
		FROM emr_records r
		JOIN emr_templates t ON t.id=r.template_id
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
		SELECT b.quality_rule_id, q.code, q.name, q.rule_type,
		       b.execution_mode, b.deadline_action
		FROM emr_template_quality_rules b
		JOIN emr_quality_rules q ON q.id=b.quality_rule_id
		WHERE b.template_id=$1 AND b.is_enabled=true AND q.status='published'
		  AND b.deadline_action <> '模板发布前处理'
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

func (s *Store) previewBindings(ctx context.Context, tenantID int64, templateVersionID string) ([]binding, error) {
	bindingRows, err := s.pool.Query(ctx, `
		SELECT b.quality_rule_id, q.code, q.name, q.rule_type,
		       b.execution_mode, b.deadline_action
		FROM emr_template_quality_rules b
		JOIN emr_quality_rules q ON q.id=b.quality_rule_id
		WHERE b.template_id=$1 AND b.is_enabled=true
		  AND q.status='published'
		  AND q.tenant_id IN (0,$2)
		  AND b.deadline_action <> '模板发布前处理'
		ORDER BY b.display_order, q.code
	`, templateVersionID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load preview emr quality bindings: %w", err)
	}
	defer bindingRows.Close()
	bindings := make([]binding, 0)
	for bindingRows.Next() {
		item := binding{}
		if scanErr := bindingRows.Scan(&item.QualityRequirementID, &item.Code, &item.Name, &item.RuleType, &item.ExecutionMode, &item.DeadlineAction); scanErr != nil {
			return nil, scanErr
		}
		bindings = append(bindings, item)
	}
	return bindings, bindingRows.Err()
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
		       execution_status, incomplete_reason, handling_result, meets_deadline, evidence,
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
		if err := rows.Scan(&item.ID, &item.CheckRunID, &item.QualityRequirementID, &item.RequirementCode, &item.RequirementName, &item.ConfiguredExecutionMode, &item.ActualExecutionMode, &item.Conclusion, &item.CheckStatus, &item.ExecutionStatus, &item.IncompleteReason, &item.HandlingResult, &item.MeetsDeadline, &rawEvidence, &item.HitExplanation, &item.SuggestedHandling, &item.ManualConclusion, &item.ManualBy, &item.ManualAt); err != nil {
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
	hasUnimplemented := false
	for _, result := range results {
		if result.ExecutionStatus == "executor_not_implemented" {
			hasUnimplemented = true
			continue
		}
		if result.HandlingResult == "阻断" {
			return "需要处理"
		}
	}
	for _, result := range results {
		if result.ExecutionStatus == "executor_not_implemented" {
			continue
		}
		if result.CheckStatus != "已完成" {
			return "无法完成"
		}
		if result.Conclusion == "不符合" {
			return "存在提示"
		}
		if result.Conclusion == "无法判断" {
			return "存在提示"
		}
	}
	if hasUnimplemented {
		return "无法完成"
	}
	return "无问题"
}
