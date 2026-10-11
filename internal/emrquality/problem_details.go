package emrquality

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const currentRuleSetVersion = "2026-10-04-v2"

type ProblemDetailsFilter struct {
	TenantID     int64
	From         *time.Time
	To           *time.Time
	SectionCode  string
	RuleCode     string
	DepartmentID string
	DoctorID     string
	Result       string
	Blocking     *bool
	ManualReview *bool
	Action       string
	Page         int
	PageSize     int
}

type ProblemDetail struct {
	RecordID           string          `json:"record_id"`
	RecordNo           string          `json:"record_no"`
	PatientDisplayName string          `json:"patient_display_name"`
	StartedAt          time.Time       `json:"started_at"`
	DepartmentName     string          `json:"department_name"`
	DoctorName         string          `json:"doctor_name"`
	TotalScore         *int            `json:"total_score"`
	Grade              string          `json:"grade"`
	SectionCode        string          `json:"section_code"`
	SectionName        string          `json:"section_name"`
	RuleCode           string          `json:"rule_code"`
	RuleName           string          `json:"rule_name"`
	Result             string          `json:"result"`
	Deduction          int             `json:"deduction"`
	Blocking           bool            `json:"blocking"`
	ManualReview       bool            `json:"manual_review"`
	Message            string          `json:"message"`
	Reason             string          `json:"reason"`
	Evidence           json.RawMessage `json:"evidence"`
	Action             string          `json:"action"`
	RuleSetVersion     string          `json:"rule_set_version"`
	CompletedAt        time.Time       `json:"completed_at"`
}

type ProblemDetailsSummary struct {
	AffectedRecordCount int     `json:"affected_record_count"`
	OccurrenceCount     int     `json:"occurrence_count"`
	AverageDeduction    float64 `json:"average_deduction"`
	NotEvaluableCount   int     `json:"not_evaluable_count"`
	BlockingCount       int     `json:"blocking_count"`
	ManualReviewCount   int     `json:"manual_review_count"`
}
type ProblemCategory struct {
	Code                string  `json:"code"`
	Name                string  `json:"name"`
	AffectedRecordCount int     `json:"affected_record_count"`
	OccurrenceCount     int     `json:"occurrence_count"`
	AverageDeduction    float64 `json:"average_deduction"`
}
type ProblemRule struct {
	Code                string  `json:"code"`
	Name                string  `json:"name"`
	SectionCode         string  `json:"section_code"`
	SectionName         string  `json:"section_name"`
	AffectedRecordCount int     `json:"affected_record_count"`
	OccurrenceCount     int     `json:"occurrence_count"`
	AverageDeduction    float64 `json:"average_deduction"`
}
type ProblemDetailsResponse struct {
	RuleSetVersion string                `json:"rule_set_version"`
	Items          []ProblemDetail       `json:"items"`
	Summary        ProblemDetailsSummary `json:"summary"`
	Categories     []ProblemCategory     `json:"categories"`
	Rules          []ProblemRule         `json:"rules"`
	Page           int                   `json:"page"`
	PageSize       int                   `json:"page_size"`
	Total          int                   `json:"total"`
}

func (s *Store) ProblemDetails(ctx context.Context, f ProblemDetailsFilter) (*ProblemDetailsResponse, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 20
	}
	args := []any{f.TenantID, currentRuleSetVersion}
	// Problem classification describes the current quality state of submitted
	// records. The record projection points at the latest quality run that was
	// bound during a successful submission, so use that pointer instead of
	// aggregating every historical save/complete/archive run for the record.
	where := "r.tenant_id=$1 AND r.status IN ('已完成','待归档','已归档') AND r.current_quality_run_id=run.id AND run.rule_set_version=$2 AND run.status='completed' AND run.completed_at IS NOT NULL AND def.version=$2 AND def.status='published' AND ver.version=$2 AND ver.status='published' AND qr.result IN ('problem','not_evaluable')"
	add := func(sql string, value any) {
		args = append(args, value)
		where += fmt.Sprintf(" AND %s=$%d", sql, len(args))
	}
	if f.From != nil {
		args = append(args, *f.From)
		where += fmt.Sprintf(" AND run.completed_at >= $%d", len(args))
	}
	if f.To != nil {
		args = append(args, *f.To)
		where += fmt.Sprintf(" AND run.completed_at < $%d", len(args))
	}
	if f.SectionCode != "" {
		add("qr.section_code", f.SectionCode)
	}
	if f.RuleCode != "" {
		add("qr.rule_code", f.RuleCode)
	}
	if f.DepartmentID != "" {
		add("r.department_id::text", f.DepartmentID)
	}
	if f.DoctorID != "" {
		add("r.doctor_id::text", f.DoctorID)
	}
	if f.Result != "" {
		add("qr.result", f.Result)
	}
	if f.Action != "" {
		add("run.action", f.Action)
	}
	if f.Blocking != nil {
		add("qr.blocking", *f.Blocking)
	}
	if f.ManualReview != nil {
		add("qr.manual_review", *f.ManualReview)
	}
	base := " FROM emr_quality_results qr JOIN emr_quality_runs run ON run.id=qr.run_id JOIN emr_quality_rule_definitions def ON def.id=qr.rule_id JOIN emr_quality_rule_set_versions ver ON ver.version=def.version JOIN emr_records r ON r.id=run.record_id LEFT JOIN employees e ON e.id=r.doctor_id AND e.tenant_id=r.tenant_id AND e.deleted_at IS NULL LEFT JOIN departments d ON d.id=r.department_id AND d.tenant_id=r.tenant_id AND d.deleted_at IS NULL LEFT JOIN emr_record_score_current current_score ON current_score.record_id=r.id AND current_score.tenant_id=r.tenant_id WHERE " + where
	var total, affected, occurrences, pending, blocking, manual int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*),COUNT(DISTINCT r.id),COUNT(*) FILTER (WHERE qr.result='problem'),COUNT(*) FILTER (WHERE qr.result='not_evaluable'),COUNT(*) FILTER (WHERE qr.blocking),COUNT(*) FILTER (WHERE qr.manual_review)"+base, args...).Scan(&total, &affected, &occurrences, &pending, &blocking, &manual); err != nil {
		return nil, err
	}
	var avg float64
	if err := s.pool.QueryRow(ctx, "SELECT COALESCE(AVG(qr.deduction) FILTER (WHERE qr.result='problem'),0)"+base, args...).Scan(&avg); err != nil {
		return nil, err
	}
	categoryRows, err := s.pool.Query(ctx, "SELECT qr.section_code, CASE qr.section_code WHEN 'general' THEN '一般项目' WHEN 'chief_complaint' THEN '主诉' WHEN 'history' THEN '病史' WHEN 'physical_exam' THEN '体检' WHEN 'auxiliary_exam' THEN '辅助检查' WHEN 'diagnosis' THEN '诊断' WHEN 'treatment' THEN '处理治疗' WHEN 'doctor_signature' THEN '医师签名' WHEN 'writing_requirement' THEN '病历书写要求' ELSE qr.section_code END, COUNT(DISTINCT r.id), COUNT(*) FILTER (WHERE qr.result='problem'), COALESCE(AVG(qr.deduction) FILTER (WHERE qr.result='problem'),0)"+base+" GROUP BY qr.section_code ORDER BY qr.section_code", args...)
	if err != nil {
		return nil, err
	}
	categories := make([]ProblemCategory, 0)
	for categoryRows.Next() {
		var item ProblemCategory
		if err := categoryRows.Scan(&item.Code, &item.Name, &item.AffectedRecordCount, &item.OccurrenceCount, &item.AverageDeduction); err != nil {
			categoryRows.Close()
			return nil, err
		}
		categories = append(categories, item)
	}
	categoryRows.Close()
	ruleRows, err := s.pool.Query(ctx, "SELECT qr.rule_code, def.name, qr.section_code, CASE qr.section_code WHEN 'general' THEN '一般项目' WHEN 'chief_complaint' THEN '主诉' WHEN 'history' THEN '病史' WHEN 'physical_exam' THEN '体检' WHEN 'auxiliary_exam' THEN '辅助检查' WHEN 'diagnosis' THEN '诊断' WHEN 'treatment' THEN '处理治疗' WHEN 'doctor_signature' THEN '医师签名' WHEN 'writing_requirement' THEN '病历书写要求' ELSE qr.section_code END, COUNT(DISTINCT r.id), COUNT(*) FILTER (WHERE qr.result='problem'), COALESCE(AVG(qr.deduction) FILTER (WHERE qr.result='problem'),0)"+base+" GROUP BY qr.rule_code, def.name, qr.section_code ORDER BY COUNT(DISTINCT r.id) DESC, qr.rule_code", args...)
	if err != nil {
		return nil, err
	}
	rules := make([]ProblemRule, 0)
	for ruleRows.Next() {
		var item ProblemRule
		if err := ruleRows.Scan(&item.Code, &item.Name, &item.SectionCode, &item.SectionName, &item.AffectedRecordCount, &item.OccurrenceCount, &item.AverageDeduction); err != nil {
			ruleRows.Close()
			return nil, err
		}
		rules = append(rules, item)
	}
	ruleRows.Close()
	rows, err := s.pool.Query(ctx, "SELECT r.id,COALESCE(r.patient_snapshot_json->>'record_no',r.id::text),COALESCE(r.patient_snapshot_json->>'name',''),r.started_at,COALESCE(NULLIF(d.name,''),NULLIF(e.medical_department_code,''),''),COALESCE(NULLIF(e.full_name,''),NULLIF(e.name,''),NULLIF(e.username,''),''),COALESCE(NULLIF(current_score.total_score,0),NULLIF(run.total_score,0)),COALESCE(NULLIF(current_score.grade,''),run.grade),qr.section_code,CASE qr.section_code WHEN 'general' THEN '一般项目' WHEN 'chief_complaint' THEN '主诉' WHEN 'history' THEN '病史' WHEN 'physical_exam' THEN '体检' WHEN 'auxiliary_exam' THEN '辅助检查' WHEN 'diagnosis' THEN '诊断' WHEN 'treatment' THEN '处理治疗' WHEN 'doctor_signature' THEN '医师签名' WHEN 'writing_requirement' THEN '病历书写要求' ELSE qr.section_code END,qr.rule_code,def.name,qr.result,qr.deduction,qr.blocking,qr.manual_review,qr.message,qr.reason,qr.evidence,run.action,run.rule_set_version,run.completed_at"+base+fmt.Sprintf(" ORDER BY run.completed_at DESC,qr.id LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2), append(args, f.PageSize, (f.Page-1)*f.PageSize)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProblemDetail{}
	for rows.Next() {
		var x ProblemDetail
		if err := rows.Scan(&x.RecordID, &x.RecordNo, &x.PatientDisplayName, &x.StartedAt, &x.DepartmentName, &x.DoctorName, &x.TotalScore, &x.Grade, &x.SectionCode, &x.SectionName, &x.RuleCode, &x.RuleName, &x.Result, &x.Deduction, &x.Blocking, &x.ManualReview, &x.Message, &x.Reason, &x.Evidence, &x.Action, &x.RuleSetVersion, &x.CompletedAt); err != nil {
			return nil, err
		}
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &ProblemDetailsResponse{RuleSetVersion: currentRuleSetVersion, Items: items, Summary: ProblemDetailsSummary{AffectedRecordCount: affected, OccurrenceCount: occurrences, AverageDeduction: avg, NotEvaluableCount: pending, BlockingCount: blocking, ManualReviewCount: manual}, Categories: categories, Rules: rules, Page: f.Page, PageSize: f.PageSize, Total: total}, nil
}
