package emrscore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
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
		SELECT record_id, tenant_id, snapshot_id, score_version, status,
		       total_score, grade, program_score, program_deduct_points,
		       major_program_score, direct_grade, direct_grade_reason,
		       pending_sections, failure_reason, scored_at, updated_at
		FROM emr_record_score_current
		WHERE record_id=$1 AND tenant_id=$2
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
