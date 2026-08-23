package compliance

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

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ListRules(ctx context.Context, tenantID int64, scope string) ([]*Rule, error) {
	query := `
		SELECT id, tenant_id, code, name, category, scope, enabled, severity, is_built_in,
		       trigger_type, conditions, description, legal_basis, suggested_script,
		       examples_json, created_at, updated_at
		FROM compliance_rules
		WHERE deleted_at IS NULL
		  AND (tenant_id = 0 OR tenant_id = $1)
	`
	args := []any{tenantID}
	if strings.TrimSpace(scope) != "" {
		query += ` AND scope = $2`
		args = append(args, strings.TrimSpace(scope))
	} else {
		query += ` AND scope IN ('communication', 'content')`
	}
	query += `
		ORDER BY is_built_in DESC, severity ASC, created_at ASC, id ASC
	`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list compliance rules: %w", err)
	}
	defer rows.Close()

	var rules []*Rule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func (s *Store) GetRule(ctx context.Context, tenantID int64, id string) (*Rule, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, code, name, category, scope, enabled, severity, is_built_in,
		       trigger_type, conditions, description, legal_basis, suggested_script,
		       examples_json, created_at, updated_at
		FROM compliance_rules
		WHERE deleted_at IS NULL
		  AND id = $1
		  AND (tenant_id = 0 OR tenant_id = $2)
	`, id, tenantID)
	rule, err := scanRule(row)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("rule not found")
	}
	if err != nil {
		return nil, fmt.Errorf("get compliance rule: %w", err)
	}
	return rule, nil
}

func (s *Store) CreateRule(ctx context.Context, tenantID int64, rule *Rule) (*Rule, error) {
	conditionsJSON, err := json.Marshal(rule.Conditions)
	if err != nil {
		return nil, fmt.Errorf("marshal conditions: %w", err)
	}
	examplesJSON, err := json.Marshal(rule.Examples)
	if err != nil {
		return nil, fmt.Errorf("marshal examples: %w", err)
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO compliance_rules (
			id, tenant_id, code, name, category, scope, enabled, severity, is_built_in,
			trigger_type, conditions, description, legal_basis, suggested_script,
			examples_json, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, FALSE,
			$9, $10::jsonb, $11, $12, $13, $14::jsonb, NOW(), NOW()
		)
		RETURNING id, tenant_id, code, name, category, scope, enabled, severity, is_built_in,
		          trigger_type, conditions, description, legal_basis, suggested_script,
		          examples_json, created_at, updated_at
	`, rule.ID, tenantID, rule.Code, rule.Name, rule.Category, rule.Scope, rule.Enabled, rule.Severity,
		rule.TriggerType, string(conditionsJSON), rule.Description, rule.LegalBasis, rule.SuggestedScript, string(examplesJSON))
	created, err := scanRule(row)
	if err != nil {
		return nil, fmt.Errorf("create compliance rule: %w", err)
	}
	return created, nil
}

func (s *Store) UpdateRule(ctx context.Context, tenantID int64, id string, req UpdateRuleRequest) (*Rule, error) {
	existing, err := s.GetRule(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if existing.IsBuiltIn && req.Code != nil {
		return nil, fmt.Errorf("built-in rule code cannot be changed")
	}
	if existing.IsBuiltIn && existing.TenantID == 0 {
		if req.Code != nil {
			return nil, fmt.Errorf("built-in rule code cannot be changed")
		}
	}

	next := *existing
	if req.Code != nil {
		next.Code = strings.TrimSpace(*req.Code)
	}
	if req.Name != nil {
		next.Name = strings.TrimSpace(*req.Name)
	}
	if req.Category != nil {
		next.Category = strings.TrimSpace(*req.Category)
	}
	if req.Scope != nil {
		next.Scope = strings.TrimSpace(*req.Scope)
	}
	if req.Enabled != nil {
		next.Enabled = *req.Enabled
	}
	if req.Severity != nil {
		next.Severity = strings.TrimSpace(*req.Severity)
	}
	if req.TriggerType != nil {
		next.TriggerType = strings.TrimSpace(*req.TriggerType)
	}
	if req.Conditions != nil {
		next.Conditions = *req.Conditions
	}
	if req.Description != nil {
		next.Description = *req.Description
	}
	if req.LegalBasis != nil {
		next.LegalBasis = *req.LegalBasis
	}
	if req.SuggestedScript != nil {
		next.SuggestedScript = *req.SuggestedScript
	}
	if req.Examples != nil {
		next.Examples = *req.Examples
	}

	conditionsJSON, err := json.Marshal(next.Conditions)
	if err != nil {
		return nil, fmt.Errorf("marshal conditions: %w", err)
	}
	examplesJSON, err := json.Marshal(next.Examples)
	if err != nil {
		return nil, fmt.Errorf("marshal examples: %w", err)
	}

	row := s.pool.QueryRow(ctx, `
		UPDATE compliance_rules
		SET code = $3,
		    name = $4,
		    category = $5,
		    scope = $6,
		    enabled = $7,
		    severity = $8,
		    trigger_type = $9,
		    conditions = $10::jsonb,
		    description = $11,
		    legal_basis = $12,
		    suggested_script = $13,
		    examples_json = $14::jsonb,
		    updated_at = NOW()
		WHERE deleted_at IS NULL
		  AND id = $1
		  AND (tenant_id = $2 OR (tenant_id = 0 AND is_built_in = TRUE))
		RETURNING id, tenant_id, code, name, category, scope, enabled, severity, is_built_in,
		          trigger_type, conditions, description, legal_basis, suggested_script,
		          examples_json, created_at, updated_at
	`, id, tenantID, next.Code, next.Name, next.Category, next.Scope, next.Enabled, next.Severity,
		next.TriggerType, string(conditionsJSON), next.Description, next.LegalBasis, next.SuggestedScript, string(examplesJSON))
	updated, err := scanRule(row)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("rule not found")
	}
	if err != nil {
		return nil, fmt.Errorf("update compliance rule: %w", err)
	}
	return updated, nil
}

func (s *Store) DeleteRule(ctx context.Context, tenantID int64, id string) error {
	cmd, err := s.pool.Exec(ctx, `
		UPDATE compliance_rules
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE deleted_at IS NULL
		  AND id = $1
		  AND tenant_id = $2
		  AND is_built_in = FALSE
	`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete compliance rule: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("rule not found or cannot be deleted")
	}
	return nil
}

func (s *Store) RestoreBuiltinRules(ctx context.Context) ([]*Rule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin restore builtin rules: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		DELETE FROM compliance_rules
		WHERE tenant_id = 0
		  AND is_built_in = TRUE
	`); err != nil {
		return nil, fmt.Errorf("delete builtin rules: %w", err)
	}

	for _, seed := range builtinRuleSeeds() {
		conditionsJSON, err := json.Marshal(seed.Conditions)
		if err != nil {
			return nil, fmt.Errorf("marshal builtin conditions: %w", err)
		}
		examplesJSON, err := json.Marshal(seed.Examples)
		if err != nil {
			return nil, fmt.Errorf("marshal builtin examples: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO compliance_rules (
				id, tenant_id, code, name, category, scope, enabled, severity, is_built_in,
				trigger_type, conditions, description, legal_basis, suggested_script,
				examples_json, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, TRUE, $7, TRUE,
				$8, $9::jsonb, $10, $11, $12, $13::jsonb, NOW(), NOW()
			)
		`, seed.ID, int64(0), seed.Code, seed.Name, seed.Category, seed.Scope, seed.Severity,
			seed.TriggerType, string(conditionsJSON), seed.Description, seed.LegalBasis, seed.SuggestedScript, string(examplesJSON)); err != nil {
			return nil, fmt.Errorf("insert builtin rule %s: %w", seed.ID, err)
		}
	}

	rows, err := tx.Query(ctx, `
		SELECT id, tenant_id, code, name, category, scope, enabled, severity, is_built_in,
		       trigger_type, conditions, description, legal_basis, suggested_script,
		       examples_json, created_at, updated_at
		FROM compliance_rules
		WHERE deleted_at IS NULL
		  AND tenant_id = 0
		ORDER BY is_built_in DESC, severity ASC, created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query rebuilt rules: %w", err)
	}
	defer rows.Close()

	var rules []*Rule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit restore builtin rules: %w", err)
	}
	return rules, nil
}

func (s *Store) EnsureBuiltinRules(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ensure builtin rules: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, seed := range builtinRuleSeeds() {
		conditionsJSON, err := json.Marshal(seed.Conditions)
		if err != nil {
			return fmt.Errorf("marshal builtin conditions: %w", err)
		}
		examplesJSON, err := json.Marshal(seed.Examples)
		if err != nil {
			return fmt.Errorf("marshal builtin examples: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO compliance_rules (
				id, tenant_id, code, name, category, scope, enabled, severity, is_built_in,
				trigger_type, conditions, description, legal_basis, suggested_script,
				examples_json, created_at, updated_at
			) VALUES (
				$1, 0, $2, $3, $4, $5, TRUE, $6, TRUE,
				$7, $8::jsonb, $9, $10, $11, $12::jsonb, NOW(), NOW()
			)
			ON CONFLICT (id) DO UPDATE
			SET code = EXCLUDED.code,
			    name = EXCLUDED.name,
			    category = EXCLUDED.category,
			    scope = EXCLUDED.scope,
			    enabled = TRUE,
			    severity = EXCLUDED.severity,
			    is_built_in = TRUE,
			    trigger_type = EXCLUDED.trigger_type,
			    conditions = EXCLUDED.conditions,
			    description = EXCLUDED.description,
			    legal_basis = EXCLUDED.legal_basis,
			    suggested_script = EXCLUDED.suggested_script,
			    examples_json = EXCLUDED.examples_json,
			    deleted_at = NULL,
			    updated_at = NOW()
		`, seed.ID, seed.Code, seed.Name, seed.Category, seed.Scope, seed.Severity,
			seed.TriggerType, string(conditionsJSON), seed.Description, seed.LegalBasis, seed.SuggestedScript, string(examplesJSON)); err != nil {
			return fmt.Errorf("upsert builtin rule %s: %w", seed.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ensure builtin rules: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRule(row rowScanner) (*Rule, error) {
	var (
		rule           Rule
		conditionsJSON []byte
		examplesJSON   []byte
	)
	err := row.Scan(
		&rule.ID,
		&rule.TenantID,
		&rule.Code,
		&rule.Name,
		&rule.Category,
		&rule.Scope,
		&rule.Enabled,
		&rule.Severity,
		&rule.IsBuiltIn,
		&rule.TriggerType,
		&conditionsJSON,
		&rule.Description,
		&rule.LegalBasis,
		&rule.SuggestedScript,
		&examplesJSON,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	rule.Conditions = JSONMap{}
	if len(conditionsJSON) > 0 {
		if err := json.Unmarshal(conditionsJSON, &rule.Conditions); err != nil {
			return nil, fmt.Errorf("unmarshal rule conditions: %w", err)
		}
	}
	rule.Examples = RuleExamples{Risky: []string{}, Safe: []string{}}
	if len(examplesJSON) > 0 {
		if err := json.Unmarshal(examplesJSON, &rule.Examples); err != nil {
			return nil, fmt.Errorf("unmarshal rule examples: %w", err)
		}
	}
	return &rule, nil
}

func newRuleID() string {
	return fmt.Sprintf("rule-%d", time.Now().UnixNano())
}
