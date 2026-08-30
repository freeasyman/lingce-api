package emrquality

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const selectColumns = `id, tenant_id, code, name, rule_type, quality_group, source_name, source_version,
 evaluated_fact, pass_condition, precondition, evidence_basis, status, created_by,
 published_at, published_by, disabled_at, disabled_by, created_at`

func scan(row pgx.Row) (*QualityRequirement, error) {
	item := &QualityRequirement{}
	err := row.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.RuleType, &item.QualityGroup,
		&item.SourceName, &item.SourceVersion, &item.EvaluatedFact, &item.PassCondition, &item.Precondition,
		&item.EvidenceBasis, &item.Status, &item.CreatedBy, &item.PublishedAt, &item.PublishedBy,
		&item.DisabledAt, &item.DisabledBy, &item.CreatedAt)
	return item, err
}

func (s *Store) List(ctx context.Context, req ListRequest) ([]*QualityRequirement, error) {
	where := []string{"(tenant_id IS NULL OR tenant_id = $1)"}
	args := []any{req.TenantID}
	arg := 2
	if strings.TrimSpace(req.Group) != "" {
		where = append(where, fmt.Sprintf("quality_group = $%d", arg))
		args = append(args, req.Group)
		arg++
	}
	if strings.TrimSpace(req.RuleType) != "" {
		where = append(where, fmt.Sprintf("rule_type = $%d", arg))
		args = append(args, req.RuleType)
		arg++
	}
	if strings.TrimSpace(req.Status) != "" {
		where = append(where, fmt.Sprintf("status = $%d", arg))
		args = append(args, req.Status)
		arg++
	}
	rows, err := s.pool.Query(ctx, "SELECT "+selectColumns+" FROM emr_quality_requirements WHERE "+strings.Join(where, " AND ")+" ORDER BY quality_group, code", args...)
	if err != nil {
		return nil, fmt.Errorf("list emr quality requirements: %w", err)
	}
	defer rows.Close()
	items := make([]*QualityRequirement, 0)
	for rows.Next() {
		item := &QualityRequirement{}
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.RuleType, &item.QualityGroup, &item.SourceName, &item.SourceVersion, &item.EvaluatedFact, &item.PassCondition, &item.Precondition, &item.EvidenceBasis, &item.Status, &item.CreatedBy, &item.PublishedAt, &item.PublishedBy, &item.DisabledAt, &item.DisabledBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Get(ctx context.Context, tenantID int64, id string) (*QualityRequirement, error) {
	item, err := scan(s.pool.QueryRow(ctx, "SELECT "+selectColumns+" FROM emr_quality_requirements WHERE id = $1 AND (tenant_id IS NULL OR tenant_id = $2)", id, tenantID))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get emr quality requirement: %w", err)
	}
	return item, nil
}

func (s *Store) Create(ctx context.Context, tenantID, actorID int64, req SaveRequest) (*QualityRequirement, error) {
	status := req.Status
	if status == "" {
		status = "draft"
	}
	var id string
	err := s.pool.QueryRow(ctx, `INSERT INTO emr_quality_requirements
 (tenant_id, code, name, rule_type, quality_group, source_name, source_version, evaluated_fact, pass_condition, precondition, evidence_basis, status, created_by, published_at, published_by)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,CASE WHEN $12='published' THEN NOW() END,CASE WHEN $12='published' THEN $13 END) RETURNING id`, tenantID, req.Code, req.Name, req.RuleType, req.QualityGroup, req.SourceName, req.SourceVersion, req.EvaluatedFact, req.PassCondition, req.Precondition, req.EvidenceBasis, status, actorID).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create emr quality requirement: %w", err)
	}
	return s.Get(ctx, tenantID, id)
}

func (s *Store) Update(ctx context.Context, tenantID int64, id string, actorID int64, req SaveRequest) (*QualityRequirement, error) {
	cmd, err := s.pool.Exec(ctx, `UPDATE emr_quality_requirements SET name=$3, rule_type=$4, quality_group=$5, source_name=$6, source_version=$7, evaluated_fact=$8, pass_condition=$9, precondition=$10, evidence_basis=$11 WHERE id=$1 AND tenant_id=$2 AND status='draft'`, id, tenantID, req.Name, req.RuleType, req.QualityGroup, req.SourceName, req.SourceVersion, req.EvaluatedFact, req.PassCondition, req.Precondition, req.EvidenceBasis)
	if err != nil {
		return nil, fmt.Errorf("update emr quality requirement: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return nil, fmt.Errorf("quality requirement not found or not editable")
	}
	_ = actorID
	return s.Get(ctx, tenantID, id)
}

func (s *Store) Disable(ctx context.Context, tenantID int64, id string, actorID int64) error {
	cmd, err := s.pool.Exec(ctx, `UPDATE emr_quality_requirements SET status='disabled', disabled_at=NOW(), disabled_by=$3 WHERE id=$1 AND (tenant_id IS NULL OR tenant_id=$2) AND status <> 'disabled'`, id, tenantID, actorID)
	if err != nil {
		return fmt.Errorf("disable emr quality requirement: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("quality requirement not found")
	}
	return nil
}

func (s *Store) EnsureBuiltin(ctx context.Context) error {
	for _, item := range builtinSeeds() {
		_, err := s.pool.Exec(ctx, `INSERT INTO emr_quality_requirements (tenant_id, code, name, rule_type, quality_group, source_name, evaluated_fact, pass_condition, precondition, evidence_basis, status, created_by, published_at)
 VALUES (NULL,$1,$2,$3,$4,'门急诊病历质量评定标准',$5,$6,$7,$8,'published',0,NOW()) ON CONFLICT ((COALESCE(tenant_id,0)), code) DO NOTHING`, item.Code, item.Name, item.RuleType, item.Group, item.Fact, item.Pass, item.Precondition, item.Evidence)
		if err != nil {
			return fmt.Errorf("seed emr quality requirement %s: %w", item.Code, err)
		}
	}
	return nil
}

func validateRequest(req SaveRequest) error {
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("code and name are required")
	}
	validType := map[string]bool{"缺项": true, "逻辑冲突": true, "风险提醒": true, "归档拦截": true, "专科要求": true}
	if !validType[req.RuleType] {
		return fmt.Errorf("invalid rule_type")
	}
	if strings.TrimSpace(req.EvaluatedFact) == "" || strings.TrimSpace(req.PassCondition) == "" || strings.TrimSpace(req.EvidenceBasis) == "" {
		return fmt.Errorf("evaluated_fact, pass_condition and evidence_basis are required")
	}
	return nil
}
