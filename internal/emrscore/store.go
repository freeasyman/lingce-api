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
	TotalScore          int            `json:"total_score"`
	Grade               string         `json:"grade"`
	ProgramScore        int            `json:"program_score"`
	ProgramDeductPoints int            `json:"program_deduct_points"`
	MajorProgramScore   int            `json:"major_program_score"`
	DirectGrade         string         `json:"direct_grade,omitempty"`
	DirectGradeReason   string         `json:"direct_grade_reason,omitempty"`
	PendingSections     []SectionCode  `json:"pending_sections,omitempty"`
	FailureReason       string         `json:"failure_reason,omitempty"`
	ScoredAt            time.Time      `json:"scored_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	Sections            []SectionScore `json:"sections"`
	Findings            []Finding      `json:"findings"`
}

func (s *Store) GetCurrent(ctx context.Context, tenantID int64, recordID string) (*CurrentScore, error) {
	item := &CurrentScore{}
	var pendingRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT c.record_id, c.tenant_id, c.snapshot_id,
		       COALESCE(r.current_score_version, c.score_version),
		       COALESCE(NULLIF(r.current_score_status, ''), c.status),
		       COALESCE(r.current_total_score, c.total_score),
		       COALESCE(NULLIF(r.current_grade, ''), c.grade),
		       c.program_score, c.program_deduct_points,
		       c.major_program_score, c.direct_grade, c.direct_grade_reason,
		       c.pending_sections, c.failure_reason, c.scored_at, c.updated_at
		FROM emr_record_score_current c
		JOIN emr_records r ON r.id=c.record_id AND r.tenant_id=c.tenant_id
		WHERE c.record_id=$1 AND c.tenant_id=$2
	`, recordID, tenantID).Scan(
		&item.RecordID, &item.TenantID, &item.SnapshotID, &item.ScoreVersion, &item.Status,
		&item.TotalScore, &item.Grade, &item.ProgramScore, &item.ProgramDeductPoints,
		&item.MajorProgramScore, &item.DirectGrade, &item.DirectGradeReason,
		&pendingRaw, &item.FailureReason, &item.ScoredAt, &item.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load current emr score: %w", err)
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
