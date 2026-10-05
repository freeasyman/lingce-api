package emrquality

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type ruleDefinition struct {
	ID              string
	Code            string
	Version         string
	SectionCode     string
	Name            string
	StandardPoints  int
	ExecutorKind    string
	EvaluationSpec  []byte
	ActionPolicy    []byte
	MessageTemplate []byte
}

func (s *Store) loadRules(ctx context.Context) ([]ruleDefinition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, rule_code, version, section_code, name, standard_points,
		       executor_kind, evaluation_spec, action_policy, message_templates
		FROM emr_quality_published_rule_set r
		WHERE version=(SELECT version FROM emr_quality_rule_set_versions WHERE status='published' ORDER BY created_at DESC, version DESC LIMIT 1)
		ORDER BY section_code, rule_code
	`)
	if err != nil {
		return nil, fmt.Errorf("load published emr quality rules: %w", err)
	}
	defer rows.Close()
	var rules []ruleDefinition
	for rows.Next() {
		var rule ruleDefinition
		if err := rows.Scan(&rule.ID, &rule.Code, &rule.Version, &rule.SectionCode, &rule.Name,
			&rule.StandardPoints, &rule.ExecutorKind, &rule.EvaluationSpec, &rule.ActionPolicy, &rule.MessageTemplate); err != nil {
			return nil, fmt.Errorf("scan emr quality rule: %w", err)
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *Store) LoadRules(ctx context.Context) ([]ruleDefinition, error) {
	return s.loadRules(ctx)
}

func (s *Store) activeVersion(ctx context.Context) (string, bool, error) {
	var version string
	var ready bool
	err := s.pool.QueryRow(ctx, `SELECT version,scoring_ready FROM emr_quality_rule_set_versions WHERE status='published' ORDER BY created_at DESC,version DESC LIMIT 1`).Scan(&version, &ready)
	return version, ready, err
}

func (s *Store) reusableRun(ctx context.Context, tenantID int64, recordID, runID, version, inputDigest string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM emr_quality_runs r
			WHERE r.id=$1 AND r.tenant_id=$2 AND r.record_id=$3
			  AND r.action='complete' AND r.status='completed'
			  AND r.rule_set_version=$4 AND r.blocking_count=0 AND r.input_digest=$5
		)
	`, runID, tenantID, recordID, version, inputDigest).Scan(&ok)
	return ok, err
}

func (s *Store) modelConfig(ctx context.Context, tenantID int64) (string, string, error) {
	var provider, model string
	err := s.pool.QueryRow(ctx, `SELECT provider,model_code FROM llm_model_configs WHERE tenant_id IN ($1,0) AND function_type='emr_quality_check' AND is_active AND deleted_at IS NULL ORDER BY (tenant_id=$1) DESC,is_default DESC,updated_at DESC,id DESC LIMIT 1`, tenantID).Scan(&provider, &model)
	return provider, model, err
}

func (s *Store) enrich(ctx context.Context, input *Context) error {
	if input.VisitType != "复诊" {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT r.id,r.started_at,r.visit_type,r.content_json FROM emr_records r JOIN emr_records current ON current.id=$1 AND current.tenant_id=$2 WHERE r.tenant_id=$2 AND r.customer_id=current.customer_id AND r.customer_id IS NOT NULL AND r.id<>current.id AND r.started_at<current.started_at AND r.status IN ('已完成','待归档','已归档') ORDER BY r.started_at DESC LIMIT 3`, input.RecordID, input.TenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, visit string
		var started any
		var raw []byte
		if err := rows.Scan(&id, &started, &visit, &raw); err != nil {
			return err
		}
		var content map[string]any
		if err := json.Unmarshal(raw, &content); err != nil {
			return err
		}
		input.History = append(input.History, map[string]any{"record_id": id, "started_at": started, "visit_type": visit, "content": content})
	}
	return rows.Err()
}

func (s *Store) evaluateICD(ctx context.Context, input Context, rule ruleDefinition) (*RuleResult, error) {
	r := baseResult(rule)
	items := diagnosisItems(diagnosisContent(input.Content))
	if len(items) == 0 {
		r.Reason = "没有诊断内容，由DX-001处理"
		return r, nil
	}
	for _, item := range items {
		name, code := valueString(item, "diagnosis_name"), valueString(item, "icd10_code")
		if code == "" {
			code = valueString(input.Content, "diagnosis_code")
		}
		if code == "" {
			code = valueString(input.Content, "icd10_code")
		}
		if strings.TrimSpace(code) == "" {
			candidates, err := s.findICD10Candidates(ctx, name)
			if err != nil {
				return nil, err
			}
			exactCount := 0
			var exactCode string
			for _, candidate := range candidates {
				if candidate.Exact {
					exactCount++
					exactCode = candidate.Code
				}
			}
			if exactCount == 1 {
				setDiagnosisCode(diagnosisContent(input.Content), input.Content, exactCode)
				continue
			}
			r.Result = "problem"
			r.Deduction = rule.StandardPoints
			r.Evidence = append(r.Evidence, Evidence{Source: "record.content", Path: "diagnosis", Excerpt: name})
			if len(candidates) == 0 {
				r.Reason += "诊断“" + name + "”未找到 ICD-10 目录中的对应名称。"
			} else {
				parts := make([]string, 0, len(candidates))
				for _, candidate := range candidates {
					parts = append(parts, candidate.Name+"（"+candidate.Code+"）")
				}
				r.Reason += "诊断“" + name + "”未找到唯一的 ICD-10 对应项。相近项：" + strings.Join(parts, "、") + "。请确认标准诊断名称。"
			}
			continue
		}
		var catalogName string
		var err error
		if code != "" {
			err = s.pool.QueryRow(ctx, `SELECT diagnosis_name FROM emr_icd10_diagnosis_catalog WHERE is_active AND diagnosis_code=$1 ORDER BY id LIMIT 1`, code).Scan(&catalogName)
		} else {
			err = s.pool.QueryRow(ctx, `SELECT diagnosis_name FROM emr_icd10_diagnosis_catalog WHERE is_active AND diagnosis_name=$1 ORDER BY id LIMIT 1`, name).Scan(&catalogName)
		}
		if err != nil && err != pgx.ErrNoRows {
			return nil, err
		}
		if err == pgx.ErrNoRows || normalizeDiagnosisText(name) != normalizeDiagnosisText(catalogName) {
			r.Result = "problem"
			r.Deduction = rule.StandardPoints
			r.Evidence = append(r.Evidence, Evidence{Source: "record.content", Path: "diagnosis", Excerpt: name + " " + code})
			r.Reason += "诊断“" + name + "”与ICD-10目录不匹配；填写编码“" + code + "”，目录名称“" + catalogName + "”。"
		}
	}
	return r, nil
}

func setDiagnosisCode(value any, content map[string]any, code string) {
	switch item := value.(type) {
	case map[string]any:
		item["icd10_code"] = code
	case []any:
		for _, entry := range item {
			if object, ok := entry.(map[string]any); ok && strings.TrimSpace(valueString(object, "icd10_code")) == "" {
				object["icd10_code"] = code
			}
		}
	default:
		if content != nil {
			content["diagnosis_code"] = code
		}
	}
}

type icdCandidate struct {
	Code, Name string
	Exact      bool
}

func (s *Store) findICD10Candidates(ctx context.Context, name string) ([]icdCandidate, error) {
	normalized := normalizeDiagnosisText(name)
	if normalized == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT diagnosis_code, diagnosis_name, normalized_name=$1 AS exact_match
		FROM emr_icd10_diagnosis_catalog
		WHERE is_active AND (normalized_name=$1 OR normalized_name ILIKE '%' || $1 || '%' OR $1 ILIKE '%' || normalized_name || '%')
		ORDER BY CASE WHEN normalized_name=$1 THEN 0 ELSE 1 END, length(diagnosis_name), diagnosis_code
		LIMIT 5
	`, normalized)
	if err != nil {
		return nil, fmt.Errorf("find ICD-10 candidates: %w", err)
	}
	defer rows.Close()
	items := make([]icdCandidate, 0, 5)
	for rows.Next() {
		var item icdCandidate
		if err := rows.Scan(&item.Code, &item.Name, &item.Exact); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeDiagnosisText(value string) string {
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "\t", "")
	value = strings.TrimSpace(value)
	return strings.NewReplacer("（", "(", "）", ")", "，", ",", "；", ";", "：", ":", "　", "").Replace(value)
}

func (s *Store) scoringReady(ctx context.Context, version string) (bool, error) {
	var ready bool
	if err := s.pool.QueryRow(ctx, `
		SELECT scoring_ready
		FROM emr_quality_rule_set_versions
		WHERE version=$1 AND status='published'
	`, version).Scan(&ready); err != nil {
		return false, fmt.Errorf("load quality rule set state: %w", err)
	}
	return ready, nil
}

func (s *Store) saveReport(ctx context.Context, report *Report) error {
	if report == nil {
		return fmt.Errorf("quality report is nil")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin quality report transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := saveReportTx(ctx, tx, report); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit quality report: %w", err)
	}
	return nil
}

type qualityDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func saveReportTx(ctx context.Context, db qualityDB, report *Report) error {
	var runID string
	if err := db.QueryRow(ctx, `
		INSERT INTO emr_quality_runs
		(tenant_id, record_id, snapshot_id, action, rule_set_version, status,
		 overall_result, total_score, grade, major_score, blocking_count, pending_count,
		 model_audit, input_digest, started_at, completed_at)
		VALUES ($1,$2,$3,$4,$5,CASE WHEN COALESCE($12::jsonb->>'failure_reason','')<>'' THEN 'failed' ELSE 'completed' END,$6,$7,$8,$9,$10,$11,$12::jsonb,$13,NOW(),NOW())
		RETURNING id
	`, report.TenantID, report.RecordID, report.SnapshotID, string(report.Action), report.RuleSetVersion,
		report.OverallResult, report.TotalScore, report.Grade, report.MajorScore, report.BlockingCount, report.PendingCount, marshalModelAudit(report.ModelAudit, report.FailureReason), report.InputDigest).Scan(&runID); err != nil {
		return fmt.Errorf("insert quality run: %w", err)
	}
	report.RunID = runID
	for _, result := range report.Results {
		evidence, marshalErr := json.Marshal(result.Evidence)
		if marshalErr != nil {
			return fmt.Errorf("marshal quality evidence: %w", marshalErr)
		}
		var ruleID string
		if err := db.QueryRow(ctx, `
			SELECT id FROM emr_quality_rule_definitions
			WHERE rule_code=$1 AND version=$2
		`, result.RuleCode, report.RuleSetVersion).Scan(&ruleID); err != nil {
			return fmt.Errorf("load quality rule %s: %w", result.RuleCode, err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO emr_quality_results
			(run_id, rule_id, rule_code, section_code, result, execution_kind,
			 evidence, reason, deduction, message, blocking, manual_review)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12)
		`, runID, ruleID, result.RuleCode, result.SectionCode, result.Result, result.ExecutionKind,
			string(evidence), result.Reason, result.Deduction, result.Message, result.Blocking, result.ManualReview); err != nil {
			return fmt.Errorf("insert quality result %s: %w", result.RuleCode, err)
		}
	}
	if (report.Action == ActionComplete || report.Action == ActionArchive) && report.ScoreReady {
		if err := saveCurrentScore(ctx, db, report); err != nil {
			return err
		}
	}
	return nil
}

func saveCurrentScore(ctx context.Context, db qualityDB, report *Report) error {
	if _, err := db.Exec(ctx, `
		INSERT INTO emr_record_score_current
		(record_id, tenant_id, snapshot_id, score_version, status, total_score, grade,
		 program_score, program_deduct_points, major_program_score, pending_sections,
		 scored_at, updated_at)
		VALUES ($1,$2,$3,$4,CASE WHEN jsonb_array_length($9::jsonb) > 0 THEN '待人工复核' ELSE '已完成' END,$5,$6,$5,$7,$8,$9::jsonb,NOW(),NOW())
		ON CONFLICT (record_id) DO UPDATE SET
		 tenant_id=EXCLUDED.tenant_id, snapshot_id=EXCLUDED.snapshot_id,
		 score_version=EXCLUDED.score_version, status=EXCLUDED.status,
		 total_score=EXCLUDED.total_score, grade=EXCLUDED.grade,
		 program_score=EXCLUDED.program_score, program_deduct_points=EXCLUDED.program_deduct_points,
		 major_program_score=EXCLUDED.major_program_score, pending_sections=EXCLUDED.pending_sections,
		 scored_at=EXCLUDED.scored_at, updated_at=EXCLUDED.updated_at
	`, report.RecordID, report.TenantID, report.SnapshotID, report.RuleSetVersion,
		report.TotalScore, report.Grade, totalDeduction(report), report.MajorScore, marshalStrings(report.PendingSections)); err != nil {
		return fmt.Errorf("save current emr score: %w", err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM emr_record_score_current_items WHERE record_id=$1`, report.RecordID); err != nil {
		return fmt.Errorf("clear current emr score items: %w", err)
	}
	for index, section := range report.Sections {
		findings := make([]map[string]any, 0, len(section.Findings))
		for _, finding := range section.Findings {
			findings = append(findings, map[string]any{
				"code": finding.RuleCode, "section": finding.SectionCode, "title": finding.Reason,
				"message": finding.Message, "evidence": finding.Evidence, "deduct_points": finding.Deduction,
				"source": finding.ExecutionKind, "needs_manual_review": finding.ManualReview,
			})
		}
		raw, err := json.Marshal(findings)
		if err != nil {
			return fmt.Errorf("marshal score findings: %w", err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO emr_record_score_current_items
			(tenant_id, record_id, section_code, section_name, standard_score, score,
			 program_deduct_points, pending, findings, sort_order, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,NOW())
		`, report.TenantID, report.RecordID, section.Code, section.Name, section.Standard,
			section.Score, section.Deduction, section.Pending, string(raw), index); err != nil {
			return fmt.Errorf("save score section %s: %w", section.Code, err)
		}
	}
	return nil
}

func totalDeduction(report *Report) int { return 100 - report.TotalScore }

func marshalStrings(values []string) string {
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func marshalModelAudit(audit map[string]any, failure string) string {
	if audit == nil {
		audit = map[string]any{}
	}
	if failure != "" {
		audit["failure_reason"] = failure
	}
	raw, err := json.Marshal(audit)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
