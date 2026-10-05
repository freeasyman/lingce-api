package emrscore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func (s *Store) SearchICD10(ctx context.Context, query string) ([]ICD10Candidate, error) {
	query = normalizeICD10Text(query)
	if query == "" {
		return []ICD10Candidate{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT diagnosis_code, diagnosis_name
		FROM emr_icd10_diagnosis_catalog
		WHERE is_active AND (normalized_name=$1 OR diagnosis_name ILIKE '%' || $2 || '%')
		ORDER BY CASE WHEN normalized_name=$1 THEN 0 ELSE 1 END, length(diagnosis_name), diagnosis_code
		LIMIT 20
	`, query, query)
	if err != nil {
		return nil, fmt.Errorf("search ICD-10 diagnoses: %w", err)
	}
	defer rows.Close()
	items := make([]ICD10Candidate, 0)
	for rows.Next() {
		var item ICD10Candidate
		if err := rows.Scan(&item.Code, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeICD10Text(value string) string {
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "\t", "")
	value = strings.TrimSpace(value)
	return strings.NewReplacer("（", "(", "）", ")", "，", ",", "；", ";", "：", ":", "　", "").Replace(value)
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type CurrentScore struct {
	RecordID            string         `json:"record_id"`
	TenantID            int64          `json:"tenant_id"`
	SnapshotID          *string        `json:"snapshot_id,omitempty"`
	ScoreVersion        string         `json:"score_version"`
	Status              string         `json:"status"`
	TotalScore          *int           `json:"total_score,omitempty"`
	Grade               *string        `json:"grade,omitempty"`
	ProgramScore        int            `json:"program_score"`
	ProgramDeductPoints int            `json:"program_deduct_points"`
	MajorProgramScore   int            `json:"major_program_score"`
	DirectGrade         string         `json:"direct_grade,omitempty"`
	DirectGradeReason   string         `json:"direct_grade_reason,omitempty"`
	PendingSections     []SectionCode  `json:"pending_sections,omitempty"`
	FailureReason       string         `json:"failure_reason,omitempty"`
	ScoredAt            time.Time      `json:"scored_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	QualityRunID        *string        `json:"quality_run_id,omitempty"`
	Sections            []SectionScore `json:"sections"`
	Findings            []Finding      `json:"findings"`
}

func (s *Store) GetCurrent(ctx context.Context, tenantID int64, recordID string) (*CurrentScore, error) {
	item := &CurrentScore{}
	var pendingRaw []byte
	var hasScore bool
	err := s.pool.QueryRow(ctx, `
		SELECT r.id, r.tenant_id, r.current_snapshot_id,
		       COALESCE(NULLIF(r.current_score_version, ''), c.score_version, ''),
		       COALESCE(NULLIF(r.current_score_status, ''), c.status, '未评分'),
			CASE WHEN COALESCE(NULLIF(r.current_score_status, ''), c.status, '未评分') IN ('已完成', '待人工复核') THEN COALESCE(r.current_total_score, c.total_score) END,
			CASE WHEN COALESCE(NULLIF(r.current_score_status, ''), c.status, '未评分') IN ('已完成', '待人工复核') THEN NULLIF(COALESCE(NULLIF(r.current_grade, ''), c.grade, ''), '') END,
		       COALESCE(c.program_score, 0), COALESCE(c.program_deduct_points, 0),
		       COALESCE(c.major_program_score, 0), COALESCE(c.direct_grade, ''), COALESCE(c.direct_grade_reason, ''),
		       COALESCE(c.pending_sections, '[]'::jsonb), COALESCE(c.failure_reason, ''),
		       COALESCE(c.scored_at, r.updated_at, r.created_at),
		       COALESCE(c.updated_at, r.updated_at, r.created_at), r.current_quality_run_id,
		       (r.current_quality_run_id IS NOT NULL OR c.record_id IS NOT NULL)
		FROM emr_records r
		LEFT JOIN emr_record_score_current c ON c.record_id=r.id AND c.tenant_id=r.tenant_id
		WHERE r.id=$1 AND r.tenant_id=$2
	`, recordID, tenantID).Scan(
		&item.RecordID, &item.TenantID, &item.SnapshotID, &item.ScoreVersion, &item.Status,
		&item.TotalScore, &item.Grade, &item.ProgramScore, &item.ProgramDeductPoints,
		&item.MajorProgramScore, &item.DirectGrade, &item.DirectGradeReason,
		&pendingRaw, &item.FailureReason, &item.ScoredAt, &item.UpdatedAt, &item.QualityRunID, &hasScore,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load current emr score: %w", err)
	}
	if !hasScore {
		return nil, nil
	}
	_ = json.Unmarshal(pendingRaw, &item.PendingSections)
	item.Sections, item.Findings, err = s.loadItems(ctx, tenantID, recordID)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Store) loadItems(ctx context.Context, tenantID int64, recordID string) ([]SectionScore, []Finding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT section_code, standard_score, score, program_deduct_points,
		       pending, findings
		FROM emr_record_score_current_items
		WHERE tenant_id=$1 AND record_id=$2
		ORDER BY sort_order, id
	`, tenantID, recordID)
	if err != nil {
		return nil, nil, fmt.Errorf("load emr score sections: %w", err)
	}
	defer rows.Close()
	sections := make([]SectionScore, 0)
	findings := make([]Finding, 0)
	for rows.Next() {
		var section SectionScore
		var raw []byte
		if err := rows.Scan(&section.Section, &section.Standard, &section.ProgramScore,
			&section.DeductPoints, &section.Pending, &raw); err != nil {
			return nil, nil, fmt.Errorf("scan emr score section: %w", err)
		}
		section.Findings = make([]Finding, 0)
		_ = json.Unmarshal(raw, &section.Findings)
		sections = append(sections, section)
		findings = append(findings, section.Findings...)
	}
	return sections, findings, rows.Err()
}
