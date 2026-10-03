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
		SELECT 'field:' || f.code || ':rule:' || (rule_item.rule ->> 'code'),
		       'field.' || f.code || '.' || (rule_item.rule ->> 'code'),
		       COALESCE(NULLIF(rule_item.rule ->> 'name', ''), f.name || '：' || (rule_item.rule ->> 'code')),
		       COALESCE(NULLIF(rule_item.rule ->> 'rule_type', ''),
		                CASE WHEN rule_item.rule ->> 'code' IN ('required', 'present') THEN '缺项' ELSE '风险提醒' END),
		       rule_item.rule ->> 'execution',
		       COALESCE(NULLIF(rule_item.rule ->> 'deadline_action', ''), '归档前处理'),
		       COALESCE(NULLIF(rule_item.rule ->> 'quality_group', ''), '栏位中心化质量规则'),
		       COALESCE(rule_item.rule ->> 'evaluated_fact', ''),
		       COALESCE(rule_item.rule ->> 'pass_condition', ''),
		       COALESCE(rule_item.rule ->> 'precondition', ''),
		       COALESCE(rule_item.rule ->> 'evidence_basis', ''),
		       COALESCE(NULLIF(rule_item.rule ->> 'source_version', ''), '2026-09-19-field-centric'),
		       f.code, f.name, tf.is_required,
		       rule_item.rule ->> 'code',
		       COALESCE(rule_item.rule ->> 'fact_code', ''),
		       COALESCE(NULLIF(rule_item.rule ->> 'name', ''), f.name || '：' || (rule_item.rule ->> 'code')),
		       COALESCE(rule_item.rule ->> 'executor', ''),
		       COALESCE(rule_item.rule -> 'parameters', '{}'::jsonb),
		       COALESCE(rule_item.rule -> 'related_fields', '[]'::jsonb),
		       COALESCE(rule_item.rule -> 'skip_if', '[]'::jsonb),
		       COALESCE(rule_item.rule ->> 'llm_instruction', '')
		FROM emr_template_fields tf
		JOIN emr_fields f ON f.id=tf.field_id
		CROSS JOIN LATERAL jsonb_array_elements(f.quality_rules) AS rule_item(rule)
		WHERE tf.template_id=$1
		  AND tf.is_visible=true
		  AND COALESCE((rule_item.rule ->> 'enabled')::boolean, false)=true
		ORDER BY tf.display_order, f.code, rule_item.rule ->> 'code'
	`, versionID)
	if err != nil {
		return nil, fmt.Errorf("load emr quality bindings: %w", err)
	}
	defer rows.Close()
	items := make([]previewBinding, 0)
	for rows.Next() {
		var item previewBinding
		var execution, fieldCode, fieldName, ruleCode, factCode, ruleName, executor, llmInstruction string
		var fieldRequired bool
		var paramsRaw, relatedRaw, skipIfRaw []byte
		if err := rows.Scan(&item.QualityRequirementID, &item.Code, &item.Name, &item.RuleType,
			&execution, &item.DeadlineAction, &item.QualityGroup, &item.EvaluatedFact,
			&item.PassCondition, &item.Precondition, &item.EvidenceBasis, &item.SourceVersion,
			&fieldCode, &fieldName, &fieldRequired, &ruleCode, &factCode, &ruleName, &executor,
			&paramsRaw, &relatedRaw, &skipIfRaw, &llmInstruction); err != nil {
			return nil, err
		}
		switch execution {
		case "program":
			item.ExecutionMode = "程序判断"
		case "llm":
			item.ExecutionMode = "大模型判断"
		default:
			item.ExecutionMode = "人工判断"
		}
		item.rule = qualityRuleInput{
			QualityRequirementID: item.QualityRequirementID, Code: item.Code, Name: item.Name,
			FieldCode: fieldCode, FieldName: fieldName, LLMInstruction: llmInstruction,
			FactCode: factCode, RuleType: item.RuleType, QualityGroup: item.QualityGroup, EvaluatedFact: item.EvaluatedFact,
			PassCondition: item.PassCondition, Precondition: item.Precondition, EvidenceBasis: item.EvidenceBasis,
			SourceVersion: item.SourceVersion, ExecutionMode: item.ExecutionMode, DeadlineAction: item.DeadlineAction,
		}
		item.FieldRule = &ExecutableRule{
			FieldCode: fieldCode, FieldName: fieldName, FieldRequired: fieldRequired,
			RuleCode: ruleCode, FactCode: factCode, RuleName: ruleName, Execution: execution, Executor: executor,
			Parameters: map[string]any{}, LLMInstruction: llmInstruction,
		}
		_ = json.Unmarshal(paramsRaw, &item.FieldRule.Parameters)
		_ = json.Unmarshal(relatedRaw, &item.FieldRule.RelatedFields)
		_ = json.Unmarshal(skipIfRaw, &item.FieldRule.SkipIf)
		item.rule.RelatedFields = append([]string(nil), item.FieldRule.RelatedFields...)
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
		SELECT (
			SELECT snapshot.id::text
			FROM emr_record_snapshots snapshot
			WHERE snapshot.record_id=r.id
			ORDER BY snapshot.snapshot_no DESC
			LIMIT 1
		)
		FROM emr_records r
		WHERE r.id=$1 AND r.tenant_id=$2
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

func newCurrentQuality(req CheckRequest, templateVersionID string, startedAt time.Time) *CheckRun {
	return &CheckRun{ID: req.RecordID, TenantID: req.TenantID, RecordID: req.RecordID,
		SnapshotID: req.SnapshotID, TemplateVersionID: templateVersionID, TriggerAction: req.TriggerAction,
		Status: "执行中", OverallResult: "无法完成", StartedAt: startedAt, StartedBy: req.StartedBy}
}

func (s *Store) createRun(ctx context.Context, req CheckRequest, templateVersionID string) (*CheckRun, error) {
	startedAt := time.Now().UTC()
	run := newCurrentQuality(req, templateVersionID, startedAt)
	if err := s.upsertCurrentQuality(ctx, run, "执行中", "无法完成", nil, "", nil); err != nil {
		return nil, fmt.Errorf("initialize current emr quality: %w", err)
	}
	return run, nil
}

func (s *Store) persistRunResults(ctx context.Context, run *CheckRun, results []*CheckResult) (*CheckRun, error) {
	completedAt := time.Now().UTC()
	overall := overallResultFromChecks(results)
	run.Status, run.OverallResult, run.CompletedAt, run.Results = "已完成", overall, &completedAt, results
	if err := s.upsertCurrentQuality(ctx, run, run.Status, overall, &completedAt, "", results); err != nil {
		return s.failRun(ctx, run.ID, err)
	}
	return run, nil
}

func (s *Store) upsertCurrentQuality(ctx context.Context, run *CheckRun, status, overall string, completedAt *time.Time, failure string, results []*CheckResult) error {
	checkedAt := time.Now().UTC()
	resultCount, attentionCount, blockingCount := 0, 0, 0
	if results != nil {
		resultCount, attentionCount, blockingCount = len(results), countAttention(results), countBlocking(results)
	}
	packageData := currentQualityPackage{Status: status, OverallResult: overall, TriggerAction: run.TriggerAction,
		SnapshotID: run.SnapshotID, TemplateVersionID: run.TemplateVersionID, StartedAt: run.StartedAt,
		CompletedAt: completedAt, CheckedAt: checkedAt, FailureReason: failure, ResultCount: resultCount,
		AttentionCount: attentionCount, BlockingCount: blockingCount, Results: results}
	rawPackage, err := json.Marshal(packageData)
	if err != nil {
		return fmt.Errorf("marshal current emr quality result: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO emr_record_quality_current
		(record_id, tenant_id, snapshot_id, template_version_id, trigger_action, status, overall_result,
		 started_at, completed_at, checked_at, failure_reason, result_count, attention_count, blocking_count, result_json, updated_at)
		VALUES ($1,$2,$3::uuid,$4::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,NOW())
		ON CONFLICT (record_id) DO UPDATE SET
		 tenant_id=EXCLUDED.tenant_id, snapshot_id=EXCLUDED.snapshot_id,
		 template_version_id=EXCLUDED.template_version_id, trigger_action=EXCLUDED.trigger_action,
		 status=EXCLUDED.status, overall_result=EXCLUDED.overall_result, started_at=EXCLUDED.started_at,
		 completed_at=EXCLUDED.completed_at, checked_at=EXCLUDED.checked_at, failure_reason=EXCLUDED.failure_reason,
		 result_count=EXCLUDED.result_count, attention_count=EXCLUDED.attention_count,
		 blocking_count=EXCLUDED.blocking_count, result_json=EXCLUDED.result_json, updated_at=NOW()
	`, run.RecordID, run.TenantID, run.SnapshotID, run.TemplateVersionID, run.TriggerAction, status, overall,
		run.StartedAt, completedAt, checkedAt, failure, resultCount, attentionCount, blockingCount, string(rawPackage))
	return err
}

func (s *Store) loadContext(ctx context.Context, req CheckRequest) (checkContext, []binding, error) {
	data := checkContext{RecordID: req.RecordID, SnapshotID: req.SnapshotID, Content: map[string]any{}}
	var rawContent, rawPatient, rawEncounter []byte
	err := s.pool.QueryRow(ctx, `
		SELECT r.template_id, r.visit_type, COALESCE(t.specialty,''),
		       COALESCE(snapshot.content,'{}'::jsonb),
		       COALESCE(r.patient_snapshot_json,'{}'::jsonb),
		       COALESCE(r.encounter_context_json,'{}'::jsonb),
		       r.status, r.created_at, r.started_at, r.ended_at,
		       r.submitted_at, r.archived_at, COALESCE(r.updated_at, r.created_at), r.updated_at,
		       r.confirmed_by
		FROM emr_records r
		JOIN emr_templates t ON t.id=r.template_id
		JOIN emr_record_snapshots snapshot ON snapshot.id=$3 AND snapshot.record_id=r.id
		WHERE r.id=$2 AND r.tenant_id=$1
	`, req.TenantID, req.RecordID, req.SnapshotID).Scan(
		&data.TemplateVersionID, &data.VisitType, &data.Specialty, &rawContent,
		&rawPatient, &rawEncounter, &data.Status, &data.CreatedAt, &data.StartedAt,
		&data.EndedAt, &data.SubmittedAt, &data.ArchivedAt, &data.LastSavedAt,
		&data.UpdatedAt, &data.ConfirmedBy)
	if err != nil {
		return data, nil, fmt.Errorf("load emr check context: %w", err)
	}
	_ = json.Unmarshal(rawContent, &data.Content)
	_ = json.Unmarshal(rawPatient, &data.PatientContext)
	_ = json.Unmarshal(rawEncounter, &data.EncounterContext)
	data.ICD10Matches, data.ICD10Candidates, err = s.loadICD10DiagnosisMatches(ctx, diagnosisNames(data.Content))
	if err != nil {
		return data, nil, err
	}
	data.RecordContext = map[string]any{
		"record_id":     data.RecordID,
		"snapshot_id":   data.SnapshotID,
		"status":        data.Status,
		"template_id":   data.TemplateVersionID,
		"visit_type":    data.VisitType,
		"specialty":     data.Specialty,
		"confirmed_by":  data.ConfirmedBy,
		"created_at":    data.CreatedAt,
		"started_at":    data.StartedAt,
		"ended_at":      data.EndedAt,
		"submitted_at":  data.SubmittedAt,
		"archived_at":   data.ArchivedAt,
		"last_saved_at": data.LastSavedAt,
		"updated_at":    data.UpdatedAt,
	}
	bindingRows, err := s.pool.Query(ctx, `
		SELECT 'field:' || f.code || ':rule:' || (rule_item.rule ->> 'code') AS quality_requirement_id,
		       'field.' || f.code || '.' || (rule_item.rule ->> 'code') AS code,
		       COALESCE(NULLIF(rule_item.rule ->> 'name', ''), f.name || '：' || (rule_item.rule ->> 'code')) AS name,
		       COALESCE(NULLIF(rule_item.rule ->> 'rule_type', ''),
		                CASE WHEN rule_item.rule ->> 'code' IN ('required', 'present') THEN '缺项' ELSE '风险提醒' END) AS rule_type,
		       rule_item.rule ->> 'execution' AS execution,
		       COALESCE(NULLIF(rule_item.rule ->> 'deadline_action', ''), '归档前处理') AS deadline_action,
		       COALESCE(NULLIF(rule_item.rule ->> 'quality_group', ''), '栏位中心化质量规则') AS quality_group,
		       COALESCE(rule_item.rule ->> 'evaluated_fact', '') AS evaluated_fact,
		       COALESCE(rule_item.rule ->> 'pass_condition', '') AS pass_condition,
		       COALESCE(rule_item.rule ->> 'precondition', '') AS precondition,
		       COALESCE(rule_item.rule ->> 'evidence_basis', '') AS evidence_basis,
		       COALESCE(NULLIF(rule_item.rule ->> 'source_version', ''), '2026-09-19-field-centric') AS source_version,
		       f.code AS field_code,
		       f.name AS field_name,
		       tf.is_required,
		       rule_item.rule ->> 'code' AS rule_code,
		       COALESCE(rule_item.rule ->> 'fact_code', '') AS fact_code,
		       COALESCE(NULLIF(rule_item.rule ->> 'name', ''), f.name || '：' || (rule_item.rule ->> 'code')) AS rule_name,
		       COALESCE(rule_item.rule ->> 'executor', '') AS executor,
		       COALESCE(rule_item.rule -> 'parameters', '{}'::jsonb),
		       COALESCE(rule_item.rule -> 'related_fields', '[]'::jsonb),
		       COALESCE(rule_item.rule -> 'skip_if', '[]'::jsonb),
		       COALESCE(rule_item.rule ->> 'llm_instruction', '') AS llm_instruction
		FROM emr_template_fields tf
		JOIN emr_fields f ON f.id=tf.field_id
		CROSS JOIN LATERAL jsonb_array_elements(f.quality_rules) AS rule_item(rule)
		WHERE tf.template_id=$1
		  AND tf.is_visible=true
		  AND COALESCE((rule_item.rule ->> 'enabled')::boolean, false)=true
		ORDER BY tf.display_order, f.code, rule_item.rule ->> 'code'
	`, data.TemplateVersionID)
	if err != nil {
		return data, nil, fmt.Errorf("load emr quality bindings for check: %w", err)
	}
	defer bindingRows.Close()
	bindings := make([]binding, 0)
	for bindingRows.Next() {
		item := binding{}
		var execution, ruleCode, factCode, ruleName, executor, llmInstruction string
		var fieldRequired bool
		var paramsRaw, relatedRaw, skipIfRaw []byte
		fieldRule := &ExecutableRule{}
		if scanErr := bindingRows.Scan(
			&item.QualityRequirementID, &item.Code, &item.Name, &item.RuleType,
			&execution, &item.DeadlineAction, &item.QualityGroup, &item.EvaluatedFact,
			&item.PassCondition, &item.Precondition, &item.EvidenceBasis, &item.SourceVersion,
			&fieldRule.FieldCode, &fieldRule.FieldName, &fieldRequired,
			&ruleCode, &factCode, &ruleName, &executor, &paramsRaw, &relatedRaw, &skipIfRaw, &llmInstruction,
		); scanErr != nil {
			return data, nil, scanErr
		}
		fieldRule.FieldRequired = fieldRequired
		fieldRule.RuleCode = ruleCode
		fieldRule.FactCode = factCode
		fieldRule.RuleName = ruleName
		fieldRule.Execution = execution
		fieldRule.Executor = executor
		fieldRule.Parameters = map[string]any{}
		_ = json.Unmarshal(paramsRaw, &fieldRule.Parameters)
		_ = json.Unmarshal(relatedRaw, &fieldRule.RelatedFields)
		_ = json.Unmarshal(skipIfRaw, &fieldRule.SkipIf)
		fieldRule.LLMInstruction = llmInstruction
		switch execution {
		case "program":
			item.ExecutionMode = "程序判断"
		case "llm":
			item.ExecutionMode = "大模型判断"
		default:
			item.ExecutionMode = "人工判断"
		}
		item.FieldRule = fieldRule
		bindings = append(bindings, item)
	}
	return data, bindings, bindingRows.Err()
}

func (s *Store) loadICD10DiagnosisMatches(ctx context.Context, names []string) (map[string]ICD10DiagnosisMatch, map[string][]ICD10DiagnosisMatch, error) {
	matches := make(map[string]ICD10DiagnosisMatch)
	candidates := make(map[string][]ICD10DiagnosisMatch)
	if len(names) == 0 {
		return matches, candidates, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT diagnosis_name, diagnosis_code
		FROM public.emr_icd10_diagnosis_catalog
		WHERE catalog_code='icd10_medical_insurance_v2'
		  AND is_active=true
		  AND diagnosis_name = ANY($1::text[])
	`, names)
	if err != nil {
		return nil, nil, fmt.Errorf("load emr ICD-10 diagnosis matches: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, code string
		if err := rows.Scan(&name, &code); err != nil {
			return nil, nil, fmt.Errorf("scan emr ICD-10 diagnosis match: %w", err)
		}
		matches[name] = ICD10DiagnosisMatch{Code: code, Name: name}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate emr ICD-10 diagnosis matches: %w", err)
	}
	rows.Close()

	candidateRows, err := s.pool.Query(ctx, `
		SELECT diagnosis_name, diagnosis_code
		FROM public.emr_icd10_diagnosis_catalog
		WHERE catalog_code='icd10_medical_insurance_v2'
		  AND is_active=true
		  AND diagnosis_name ILIKE ANY(
		        ARRAY(SELECT '%' || name || '%' FROM unnest($1::text[]) AS input(name))
		      )
		ORDER BY diagnosis_name, diagnosis_code
	`, names)
	if err != nil {
		return nil, nil, fmt.Errorf("load emr ICD-10 diagnosis candidates: %w", err)
	}
	defer candidateRows.Close()
	for candidateRows.Next() {
		var name, code string
		if err := candidateRows.Scan(&name, &code); err != nil {
			return nil, nil, fmt.Errorf("scan emr ICD-10 diagnosis candidate: %w", err)
		}
		item := ICD10DiagnosisMatch{Code: code, Name: name}
		for _, input := range names {
			if strings.Contains(name, input) {
				candidates[input] = append(candidates[input], item)
			}
		}
	}
	if err := candidateRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate emr ICD-10 diagnosis candidates: %w", err)
	}
	return matches, candidates, nil
}

func (s *Store) failRun(ctx context.Context, id string, cause error) (*CheckRun, error) {
	reason := strings.TrimSpace(cause.Error())
	completedAt := time.Now().UTC()
	_, _ = s.pool.Exec(ctx, `
		UPDATE emr_record_quality_current
		SET status='执行失败', overall_result='无法完成', completed_at=$2, checked_at=$2,
		    failure_reason=$3, result_count=0, attention_count=0, blocking_count=0,
		    result_json=jsonb_build_object('status','执行失败','overall_result','无法完成',
		      'checked_at',$2::timestamptz,'failure_reason',$3,'results','[]'::jsonb), updated_at=NOW()
		WHERE record_id=$1
	`, id, completedAt, reason)
	return nil, cause
}

func (s *Store) CurrentQuality(ctx context.Context, tenantID int64, recordID string) (*CheckRun, error) {
	item := &CheckRun{}
	var rawPackage []byte
	err := s.pool.QueryRow(ctx, `
		SELECT quality.record_id::text,
		       quality.tenant_id,
		       quality.record_id::text,
		       COALESCE(quality.snapshot_id::text, ''),
		       COALESCE(quality.template_version_id::text, ''),
		       quality.trigger_action,
		       quality.status,
		       quality.overall_result,
		       COALESCE(quality.started_at, quality.checked_at),
		       quality.completed_at,
		       NULL::bigint,
		       quality.failure_reason,
		       quality.result_json
		FROM emr_record_quality_current quality
		WHERE quality.tenant_id=$1 AND quality.record_id=$2
	`, tenantID, recordID).Scan(
		&item.ID, &item.TenantID, &item.RecordID, &item.SnapshotID, &item.TemplateVersionID,
		&item.TriggerAction, &item.Status, &item.OverallResult, &item.StartedAt, &item.CompletedAt,
		&item.StartedBy, &item.FailureReason, &rawPackage)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get current emr quality: %w", err)
	}
	var packageData currentQualityPackage
	if err := json.Unmarshal(rawPackage, &packageData); err != nil {
		return nil, fmt.Errorf("decode current emr quality result: %w", err)
	}
	item.Results = packageData.Results
	return item, nil
}

func (s *Store) initializeCurrentQualityWithoutRun(ctx context.Context, req CheckRequest, templateVersionID string) error {
	startedAt := time.Now().UTC()
	rawPackage, err := json.Marshal(currentQualityPackage{
		Status:            "执行中",
		OverallResult:     "无法完成",
		TriggerAction:     req.TriggerAction,
		SnapshotID:        req.SnapshotID,
		TemplateVersionID: templateVersionID,
		StartedAt:         startedAt,
		CheckedAt:         startedAt,
		Results:           []*CheckResult{},
	})
	if err != nil {
		return fmt.Errorf("marshal current emr quality running result: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO emr_record_quality_current
		(record_id, tenant_id, snapshot_id, template_version_id, trigger_action,
		 status, overall_result, started_at, completed_at, checked_at, failure_reason,
		 result_count, attention_count, blocking_count, result_json, updated_at)
		VALUES ($1,$2,$3::uuid,$4::uuid,$5,'执行中','无法完成',$6,NULL,$6,'',
		        0,0,0,$7::jsonb,NOW())
		ON CONFLICT (record_id) DO UPDATE SET
			tenant_id=EXCLUDED.tenant_id,
			snapshot_id=EXCLUDED.snapshot_id,
			template_version_id=EXCLUDED.template_version_id,
			trigger_action=EXCLUDED.trigger_action,
			status='执行中',
			overall_result='无法完成',
			started_at=EXCLUDED.started_at,
			completed_at=NULL,
			checked_at=EXCLUDED.checked_at,
			failure_reason='',
			result_count=0,
			attention_count=0,
			blocking_count=0,
			result_json=EXCLUDED.result_json,
			updated_at=NOW()
	`, req.RecordID, req.TenantID, req.SnapshotID, templateVersionID, req.TriggerAction, startedAt, string(rawPackage))
	if err != nil {
		return fmt.Errorf("upsert current emr quality running state without run: %w", err)
	}
	return nil
}

func (s *Store) persistCurrentQualityWithoutRun(ctx context.Context, req CheckRequest, templateVersionID string, results []*CheckResult) error {
	completedAt := time.Now().UTC()
	overall := overallResultFromChecks(results)
	rawPackage, err := json.Marshal(currentQualityPackage{
		Status:            "已完成",
		OverallResult:     overall,
		TriggerAction:     req.TriggerAction,
		SnapshotID:        req.SnapshotID,
		TemplateVersionID: templateVersionID,
		StartedAt:         completedAt,
		CompletedAt:       &completedAt,
		CheckedAt:         completedAt,
		ResultCount:       len(results),
		AttentionCount:    countAttention(results),
		BlockingCount:     countBlocking(results),
		Results:           results,
	})
	if err != nil {
		return fmt.Errorf("marshal current emr quality result without run: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE emr_record_quality_current
		SET tenant_id=$2,
		    snapshot_id=$3::uuid,
		    template_version_id=$4::uuid,
		    trigger_action=$5,
		    status='已完成',
		    overall_result=$6,
		    started_at=COALESCE(started_at,NOW()),
		    completed_at=$7,
		    checked_at=$7,
		    failure_reason='',
		    result_count=$8,
		    attention_count=$9,
		    blocking_count=$10,
		    result_json=$11::jsonb,
		    updated_at=NOW()
		WHERE record_id=$1
		  AND snapshot_id=$3::uuid
	`, req.RecordID, req.TenantID, req.SnapshotID, templateVersionID, req.TriggerAction, overall, completedAt,
		len(results), countAttention(results), countBlocking(results), string(rawPackage))
	if err != nil {
		return fmt.Errorf("save current emr quality result without run: %w", err)
	}
	return nil
}

func (s *Store) markCurrentQualityFailedWithoutRun(ctx context.Context, req CheckRequest, templateVersionID, reason string) error {
	completedAt := time.Now().UTC()
	rawPackage, err := json.Marshal(currentQualityPackage{
		Status:            "执行失败",
		OverallResult:     "无法完成",
		TriggerAction:     req.TriggerAction,
		SnapshotID:        req.SnapshotID,
		TemplateVersionID: templateVersionID,
		StartedAt:         completedAt,
		CompletedAt:       &completedAt,
		CheckedAt:         completedAt,
		FailureReason:     reason,
		Results:           []*CheckResult{},
	})
	if err != nil {
		return fmt.Errorf("marshal current emr quality failed result without run: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE emr_record_quality_current
		SET status='执行失败',
		    overall_result='无法完成',
		    completed_at=$4,
		    checked_at=$4,
		    failure_reason=$5,
		    result_count=0,
		    attention_count=0,
		    blocking_count=0,
		    result_json=$6::jsonb,
		    updated_at=NOW()
		WHERE record_id=$1
		  AND tenant_id=$2
		  AND snapshot_id=$3::uuid
	`, req.RecordID, req.TenantID, req.SnapshotID, completedAt, reason, string(rawPackage))
	if err != nil {
		return fmt.Errorf("save current emr quality failure without run: %w", err)
	}
	return nil
}

func (s *Store) QualityManagementSummary(ctx context.Context, tenantID int64, filter QualityManagementFilter) (*QualityManagementSummary, error) {
	where, args := qualityManagementWhere(tenantID, filter)
	query := `
		WITH scoped_records AS (
			SELECT r.id, r.updated_at
			FROM emr_records r
			WHERE ` + where + `
		), current_quality AS (
			SELECT quality.*
			FROM emr_record_quality_current quality
			JOIN scoped_records record ON record.id=quality.record_id
			WHERE quality.tenant_id=$1
		)
		SELECT
			(SELECT COUNT(*) FROM scoped_records),
			(SELECT COUNT(*) FROM current_quality WHERE status IN ('已完成', '执行失败')),
			(SELECT COUNT(*) FROM current_quality WHERE status='已完成' AND overall_result <> '无问题'),
			(SELECT COALESCE(SUM(blocking_count), 0) FROM current_quality),
			(SELECT COUNT(*) FROM current_quality WHERE status='执行失败'),
			(SELECT COUNT(*) FROM current_quality WHERE status='执行中'),
			(SELECT COALESCE(SUM(attention_count), 0) FROM current_quality),
			(SELECT MAX(checked_at) FROM current_quality)
	`
	item := &QualityManagementSummary{}
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&item.RecordTotal, &item.CheckedRecordTotal, &item.ProblemRecordTotal, &item.BlockingResultTotal, &item.FailedCheckTotal, &item.RunningCheckTotal, &item.AttentionResultTotal, &item.LastUpdatedAt); err != nil {
		return nil, fmt.Errorf("load emr quality management summary: %w", err)
	}
	return item, nil
}

func (s *Store) QualityManagementFindings(ctx context.Context, tenantID int64, filter QualityManagementFilter) (*QualityFindingGroupList, error) {
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	where, args := qualityManagementWhere(tenantID, filter)
	attentionWhere := ""
	if filter.Scope != "all" {
		attentionWhere = "AND item.is_attention"
	}
	limitIndex := len(args) + 1
	offsetIndex := len(args) + 2
	args = append(args, filter.Limit, filter.Offset)
	query := fmt.Sprintf(`
		WITH scoped_records AS (
			SELECT r.id, r.patient_snapshot_json, r.department_id, r.doctor_id, r.status, r.updated_at
			FROM emr_records r
			WHERE %s
		), current_items AS (
			SELECT
				quality.record_id,
				quality.tenant_id,
				quality.checked_at,
				item,
				COALESCE(NULLIF(item->>'rule_code',''), item->>'requirement_code') AS rule_code,
				COALESCE(NULLIF(item->>'rule_name',''), NULLIF(item->>'requirement_name',''), item->>'requirement_code') AS rule_name,
				COALESCE(NULLIF(item->>'field_code',''), '') AS field_code,
				COALESCE(NULLIF(item->>'field_name',''), '') AS field_name,
				(item->>'conclusion' <> '符合'
				 OR item->>'check_status' <> '已完成'
				 OR item->>'handling_result' <> '允许继续') AS is_attention,
				(item->>'handling_result' IN ('阻断', '需要医生处理')) AS is_blocking,
				COALESCE(
					NULLIF(item->>'hit_explanation',''),
					NULLIF(item->>'incomplete_reason',''),
					NULLIF(item->>'suggested_handling',''),
					NULLIF(item->>'requirement_name',''),
					item->>'requirement_code'
				) AS display_message
			FROM emr_record_quality_current quality
			CROSS JOIN LATERAL jsonb_array_elements(
				COALESCE(quality.result_json->'results', '[]'::jsonb)
			) item
			WHERE quality.tenant_id=$1
		), enriched AS (
			SELECT item.item->>'id' AS id,
			       item.item->>'id' AS result_id,
			       item.record_id,
			       item.checked_at, record.patient_snapshot_json, record.department_id,
			       record.doctor_id, record.status AS record_status,
			       item.item->>'conclusion' AS conclusion,
			       item.item->>'check_status' AS check_status,
			       item.item->>'handling_result' AS handling_result,
			       item.item->>'requirement_code' AS requirement_code,
			       item.item->>'requirement_name' AS requirement_name,
			       item.item->'evidence' AS evidence,
			       item.field_code, item.field_name, item.rule_code, item.rule_name,
			       item.display_message, item.is_attention, item.is_blocking,
			       item.rule_code AS group_key,
			       item.rule_name AS display_title
			FROM current_items item
			JOIN scoped_records record ON record.id=item.record_id
			WHERE item.tenant_id=$1 %s
		), grouped AS (
			SELECT group_key, field_code, field_name, rule_code,
			       MAX(display_title) AS title,
			       COUNT(DISTINCT record_id)::int AS record_count,
			       COUNT(*)::int AS result_count,
			       COUNT(*) FILTER (WHERE handling_result='阻断')::int AS blocking_count,
			       MAX(checked_at) AS latest_checked_at
			FROM enriched
			GROUP BY group_key, field_code, field_name, rule_code
		), samples AS (
			SELECT group_key,
			       jsonb_agg(jsonb_build_object(
			         'record_id', record_id,
			         'result_id', id,
			         'patient_name', COALESCE(NULLIF(patient_snapshot_json->>'name',''), NULLIF(patient_snapshot_json->>'patient_name',''), NULLIF(patient_snapshot_json->>'full_name',''), '患者信息待确认'),
			         'doctor_id', doctor_id,
			         'department_id', department_id,
			         'record_status', record_status,
			         'field_code', field_code,
			         'field_name', field_name,
			         'conclusion', conclusion,
			         'handling_result', handling_result,
			         'message', display_message,
			         'checked_at', checked_at
			       ) ORDER BY checked_at DESC NULLS LAST, record_id) AS sample_records
			FROM (
				SELECT enriched.*, ROW_NUMBER() OVER (PARTITION BY group_key ORDER BY checked_at DESC NULLS LAST, record_id) AS sample_no
				FROM enriched
			) ranked
			WHERE sample_no <= 3
			GROUP BY group_key
		), paged AS (
			SELECT *, COUNT(*) OVER()::int AS total
			FROM grouped
			ORDER BY blocking_count DESC, record_count DESC, latest_checked_at DESC NULLS LAST, title
			LIMIT $%d OFFSET $%d
		)
		SELECT paged.group_key, field_code, field_name, rule_code, title, record_count, result_count, blocking_count, latest_checked_at, total,
		       COALESCE(samples.sample_records, '[]'::jsonb)
		FROM paged
		LEFT JOIN samples ON samples.group_key=paged.group_key
	`, where, attentionWhere, limitIndex, offsetIndex)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list emr quality management findings: %w", err)
	}
	defer rows.Close()
	items := make([]*QualityFindingGroup, 0)
	total := 0
	for rows.Next() {
		item := &QualityFindingGroup{}
		var sampleRaw []byte
		if err := rows.Scan(&item.GroupKey, &item.FieldCode, &item.FieldName, &item.RuleCode, &item.Title, &item.RecordCount, &item.ResultCount, &item.BlockingCount, &item.LatestCheckedAt, &total, &sampleRaw); err != nil {
			return nil, err
		}
		item.Severity = severityForQualityFinding(item.BlockingCount)
		_ = json.Unmarshal(sampleRaw, &item.SampleRecords)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &QualityFindingGroupList{Items: items, Total: total}, nil
}

func (s *Store) QualityManagementFindingRecords(ctx context.Context, tenantID int64, groupKey string, filter QualityManagementFilter) (*QualityFindingRecordList, error) {
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	items, total, title, err := s.qualityFindingRecords(ctx, tenantID, filter, groupKey, filter.Limit, filter.Offset)
	if err != nil {
		return nil, err
	}
	return &QualityFindingRecordList{GroupKey: groupKey, Title: title, Items: items, Total: total}, nil
}

func completedAtOrNow(value *time.Time) time.Time {
	if value != nil {
		return value.UTC()
	}
	return time.Now().UTC()
}

func countAttention(results []*CheckResult) int {
	count := 0
	for _, result := range results {
		if result == nil {
			continue
		}
		if result.Conclusion != "符合" ||
			result.CheckStatus != "已完成" ||
			result.HandlingResult != "允许继续" {
			count++
		}
	}
	return count
}

func countBlocking(results []*CheckResult) int {
	count := 0
	for _, result := range results {
		if result != nil && result.HandlingResult == "阻断" {
			count++
		}
	}
	return count
}

func stringFromEvidence(evidence map[string]any, key string) string {
	if evidence == nil {
		return ""
	}
	value, _ := evidence[key].(string)
	return strings.TrimSpace(value)
}

func qualityManagementWhere(tenantID int64, filter QualityManagementFilter) (string, []any) {
	args := []any{tenantID}
	where := "r.tenant_id=$1"
	if filter.DateFrom != nil {
		args = append(args, *filter.DateFrom)
		where += fmt.Sprintf(" AND r.updated_at >= $%d", len(args))
	}
	if filter.DateTo != nil {
		args = append(args, *filter.DateTo)
		where += fmt.Sprintf(" AND r.updated_at < $%d", len(args))
	}
	if filter.DepartmentID != nil {
		args = append(args, *filter.DepartmentID)
		where += fmt.Sprintf(" AND r.department_id = $%d", len(args))
	}
	if filter.DoctorID != nil {
		args = append(args, *filter.DoctorID)
		where += fmt.Sprintf(" AND r.doctor_id = $%d", len(args))
	}
	if strings.TrimSpace(filter.Status) != "" {
		args = append(args, strings.TrimSpace(filter.Status))
		where += fmt.Sprintf(" AND r.status = $%d", len(args))
	}
	return where, args
}

func severityForQualityFinding(blockingCount int) string {
	if blockingCount > 0 {
		return "high"
	}
	return "medium"
}

func (s *Store) qualityFindingSamples(ctx context.Context, tenantID int64, filter QualityManagementFilter, groupKey string, limit int) ([]*QualityFindingRecordSample, error) {
	items, _, _, err := s.qualityFindingRecords(ctx, tenantID, filter, groupKey, limit, 0)
	return items, err
}

func (s *Store) qualityFindingRecords(ctx context.Context, tenantID int64, filter QualityManagementFilter, groupKey string, limit, offset int) ([]*QualityFindingRecordSample, int, string, error) {
	where, args := qualityManagementWhere(tenantID, filter)
	groupIndex := len(args) + 1
	limitIndex := len(args) + 2
	offsetIndex := len(args) + 3
	args = append(args, groupKey, limit, offset)
	query := fmt.Sprintf(`
		WITH scoped_records AS (
			SELECT r.id, r.patient_snapshot_json, r.department_id, r.doctor_id, r.status, r.updated_at
			FROM emr_records r
			WHERE %s
		), current_items AS (
			SELECT
				quality.record_id,
				quality.tenant_id,
				quality.checked_at,
				item,
				COALESCE(NULLIF(item->>'rule_code',''), item->>'requirement_code') AS rule_code,
				COALESCE(NULLIF(item->>'rule_name',''), NULLIF(item->>'requirement_name',''), item->>'requirement_code') AS rule_name,
				COALESCE(NULLIF(item->>'field_code',''), '') AS field_code,
				COALESCE(NULLIF(item->>'field_name',''), '') AS field_name,
				(item->>'conclusion' <> '符合'
				 OR item->>'check_status' <> '已完成'
				 OR item->>'handling_result' <> '允许继续') AS is_attention,
				(item->>'handling_result' IN ('阻断', '需要医生处理')) AS is_blocking,
				COALESCE(
					NULLIF(item->>'hit_explanation',''),
					NULLIF(item->>'incomplete_reason',''),
					NULLIF(item->>'suggested_handling',''),
					NULLIF(item->>'requirement_name',''),
					item->>'requirement_code'
				) AS display_message
			FROM emr_record_quality_current quality
			CROSS JOIN LATERAL jsonb_array_elements(
				COALESCE(quality.result_json->'results', '[]'::jsonb)
			) item
			WHERE quality.tenant_id=$1
		), enriched AS (
			SELECT item.item->>'id' AS result_id,
			       item.record_id, item.checked_at,
			       record.patient_snapshot_json, record.department_id, record.doctor_id, record.status AS record_status,
			       item.item->>'conclusion' AS conclusion,
			       item.item->>'check_status' AS check_status,
			       item.item->>'handling_result' AS handling_result,
			       item.item->>'requirement_code' AS requirement_code,
			       item.item->>'requirement_name' AS requirement_name,
			       item.item->'evidence' AS evidence,
			       item.rule_code AS group_key,
			       item.field_code, item.field_name, item.display_message,
			       item.is_attention
			FROM current_items item
			JOIN scoped_records record ON record.id=item.record_id
			WHERE item.tenant_id=$1
			  %s
		), matched AS (
			SELECT *, COUNT(*) OVER()::int AS total
			FROM enriched
			WHERE group_key=$%d
			ORDER BY checked_at DESC NULLS LAST, record_id
			LIMIT $%d OFFSET $%d
		)
		SELECT record_id, result_id,
		       COALESCE(NULLIF(patient_snapshot_json->>'name',''), NULLIF(patient_snapshot_json->>'patient_name',''), NULLIF(patient_snapshot_json->>'full_name',''), '患者信息待确认') AS patient_name,
		       doctor_id, department_id, record_status, field_code, field_name, conclusion, handling_result, display_message, checked_at, total, display_title
		FROM matched
	`, where, qualityManagementAttentionClause(filter.Scope), groupIndex, limitIndex, offsetIndex)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, "", fmt.Errorf("list emr quality finding records: %w", err)
	}
	defer rows.Close()
	items := make([]*QualityFindingRecordSample, 0)
	total := 0
	title := ""
	for rows.Next() {
		item := &QualityFindingRecordSample{}
		if err := rows.Scan(&item.RecordID, &item.ResultID, &item.PatientName, &item.DoctorID, &item.DepartmentID, &item.RecordStatus, &item.FieldCode, &item.FieldName, &item.Conclusion, &item.HandlingResult, &item.Message, &item.CheckedAt, &total, &title); err != nil {
			return nil, 0, "", err
		}
		items = append(items, item)
	}
	return items, total, title, rows.Err()
}

func qualityManagementAttentionClause(scope string) string {
	if scope == "all" {
		return ""
	}
	return "AND item.is_attention"
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

func overallResultFromChecks(results []*CheckResult) string {
	hasIncomplete := false
	for _, result := range results {
		if result == nil {
			hasIncomplete = true
			continue
		}
		if result.HandlingResult == "阻断" {
			return "需要处理"
		}
		if result.CheckStatus != "已完成" {
			hasIncomplete = true
		}
		if result.Conclusion == "不符合" || result.Conclusion == "无法判断" {
			return "存在提示"
		}
	}
	if hasIncomplete {
		return "无法完成"
	}
	return "无问题"
}
