package emrtemplate

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

func (s *Store) ListTemplates(ctx context.Context, tenantID int64) ([]*TemplateListItemResponse, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, name, short_name, department_code, department_name, status,
		       description, version_no, is_system, updated_at
		FROM emr_templates
		WHERE deleted_at IS NULL
		  AND tenant_id = $1
		ORDER BY is_system DESC, department_name ASC, id ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list emr templates: %w", err)
	}
	defer rows.Close()

	items := make([]*TemplateListItemResponse, 0)
	for rows.Next() {
		var item TemplateListItemResponse
		var updatedAt time.Time
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.Name,
			&item.ShortName,
			&item.DepartmentCode,
			&item.DepartmentName,
			&item.Status,
			&item.Description,
			&item.VersionNo,
			&item.IsSystem,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan emr template: %w", err)
		}
		item.UpdatedAt = updatedAt.Format("2006-01-02 15:04")
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) GetTemplate(ctx context.Context, tenantID, templateID int64) (*TemplateDetailResponse, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, code, name, short_name, department_code, department_name,
		       status, description, version_no, is_system, schema_json, updated_at
		FROM emr_templates
		WHERE deleted_at IS NULL
		  AND tenant_id = $1
		  AND id = $2
	`, tenantID, templateID)

	var resp TemplateDetailResponse
	var schemaRaw []byte
	var updatedAt time.Time
	if err := row.Scan(
		&resp.ID,
		&resp.TenantID,
		&resp.Code,
		&resp.Name,
		&resp.ShortName,
		&resp.DepartmentCode,
		&resp.DepartmentName,
		&resp.Status,
		&resp.Description,
		&resp.VersionNo,
		&resp.IsSystem,
		&schemaRaw,
		&updatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get emr template: %w", err)
	}
	resp.UpdatedAt = updatedAt.Format("2006-01-02 15:04")
	resp.SchemaJSON = map[string]any{}
	if len(schemaRaw) > 0 {
		_ = json.Unmarshal(schemaRaw, &resp.SchemaJSON)
	}

	bindings, err := s.ListBindings(ctx, tenantID, templateID)
	if err != nil {
		return nil, err
	}
	resp.Bindings = bindings
	return &resp, nil
}

func (s *Store) CreateTemplate(ctx context.Context, tenantID, actorID int64, req SaveTemplateRequest) (*TemplateDetailResponse, error) {
	schemaJSON, err := json.Marshal(req.SchemaJSON)
	if err != nil {
		return nil, fmt.Errorf("marshal schema_json: %w", err)
	}
	var templateID int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO emr_templates (
			tenant_id, code, name, short_name, department_code, department_name,
			status, description, version_no, is_system, schema_json, created_by, updated_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, 1, FALSE, $9::jsonb, $10, $10, NOW(), NOW()
		)
		RETURNING id
	`, tenantID, req.Code, req.Name, req.ShortName, req.DepartmentCode, req.DepartmentName, req.Status, req.Description, string(schemaJSON), actorID).Scan(&templateID); err != nil {
		return nil, fmt.Errorf("create emr template: %w", err)
	}
	return s.GetTemplate(ctx, tenantID, templateID)
}

func (s *Store) UpdateTemplate(ctx context.Context, tenantID, templateID, actorID int64, req SaveTemplateRequest) (*TemplateDetailResponse, error) {
	schemaJSON, err := json.Marshal(req.SchemaJSON)
	if err != nil {
		return nil, fmt.Errorf("marshal schema_json: %w", err)
	}
	cmd, err := s.pool.Exec(ctx, `
		UPDATE emr_templates
		SET code = $3,
		    name = $4,
		    short_name = $5,
		    department_code = $6,
		    department_name = $7,
		    status = $8,
		    description = $9,
		    schema_json = $10::jsonb,
		    version_no = version_no + 1,
		    updated_by = $11,
		    updated_at = NOW()
		WHERE tenant_id = $1
		  AND id = $2
		  AND deleted_at IS NULL
	`, tenantID, templateID, req.Code, req.Name, req.ShortName, req.DepartmentCode, req.DepartmentName, req.Status, req.Description, string(schemaJSON), actorID)
	if err != nil {
		return nil, fmt.Errorf("update emr template: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return nil, fmt.Errorf("template not found")
	}
	return s.GetTemplate(ctx, tenantID, templateID)
}

func (s *Store) ListBindings(ctx context.Context, tenantID, templateID int64) ([]*BindingItemResponse, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.id, b.rule_id, b.rule_code, b.rule_name, b.rule_detail,
		       b.enabled, b.rule_scope, b.sort_order, b.stage_config_json,
		       COALESCE(r.id, ''), COALESCE(r.code, ''), COALESCE(r.name, ''),
		       COALESCE(r.category, ''), COALESCE(r.severity, ''), COALESCE(r.description, ''),
		       COALESCE(r.enabled, FALSE)
		FROM emr_template_rule_bindings b
		LEFT JOIN compliance_rules r ON r.id = b.rule_id AND r.deleted_at IS NULL
		WHERE b.deleted_at IS NULL
		  AND b.tenant_id = $1
		  AND b.template_id = $2
		ORDER BY b.sort_order ASC, b.id ASC
	`, tenantID, templateID)
	if err != nil {
		return nil, fmt.Errorf("list emr template bindings: %w", err)
	}
	defer rows.Close()

	items := make([]*BindingItemResponse, 0)
	for rows.Next() {
		var item BindingItemResponse
		var stageRaw []byte
		var rule RuleSummary
		if err := rows.Scan(
			&item.ID,
			&item.RuleID,
			&item.RuleCode,
			&item.RuleName,
			&item.RuleDetail,
			&item.Enabled,
			&item.RuleScope,
			&item.SortOrder,
			&stageRaw,
			&rule.ID,
			&rule.Code,
			&rule.Name,
			&rule.Category,
			&rule.Severity,
			&rule.Description,
			&rule.Enabled,
		); err != nil {
			return nil, fmt.Errorf("scan emr template binding: %w", err)
		}
		item.StageConfigJSON = BindingStageConfig{}
		if len(stageRaw) > 0 {
			_ = json.Unmarshal(stageRaw, &item.StageConfigJSON)
		}
		if strings.TrimSpace(item.RuleID) == "" {
			item.RuleID = rule.ID
		}
		item.Rule = rule
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (s *Store) ReplaceBindings(ctx context.Context, tenantID, templateID, actorID int64, items []BindingItemUpsert) ([]*BindingItemResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin replace template bindings: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		UPDATE emr_template_rule_bindings
		SET deleted_at = NOW(), updated_at = NOW(), updated_by = $3
		WHERE tenant_id = $1
		  AND template_id = $2
		  AND deleted_at IS NULL
	`, tenantID, templateID, actorID); err != nil {
		return nil, fmt.Errorf("clear template bindings: %w", err)
	}

	for _, item := range items {
		ruleID, ruleSnapshot, err := s.resolveRuleForBinding(ctx, tenantID, item)
		if err != nil {
			return nil, err
		}
		stageJSON, err := json.Marshal(item.StageConfigJSON)
		if err != nil {
			return nil, fmt.Errorf("marshal stage_config_json: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO emr_template_rule_bindings (
				tenant_id, template_id, rule_id, rule_code, rule_name, rule_detail, enabled, rule_scope,
				stage_config_json, sort_order, created_by, updated_by, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8,
				$9::jsonb, $10, $11, $11, NOW(), NOW()
			)
		`, tenantID, templateID, ruleID, ruleSnapshot.Code, ruleSnapshot.Name, ruleSnapshot.Detail, item.Enabled, item.RuleScope, string(stageJSON), item.SortOrder, actorID); err != nil {
			return nil, fmt.Errorf("insert template binding: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit replace template bindings: %w", err)
	}
	return s.ListBindings(ctx, tenantID, templateID)
}

func (s *Store) EnsureSeedTemplates(ctx context.Context, tenantID int64) error {
	var count int
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM emr_templates
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
	`, tenantID).Scan(&count); err != nil {
		return fmt.Errorf("count emr templates: %w", err)
	}
	if count > 0 {
		return s.EnsureMissingSeedBindings(ctx, tenantID)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin seed emr templates: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, seed := range builtinTemplates() {
		schemaJSON, err := json.Marshal(seed.SchemaJSON)
		if err != nil {
			return fmt.Errorf("marshal builtin template schema: %w", err)
		}
		var templateID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO emr_templates (
				tenant_id, code, name, short_name, department_code, department_name,
				status, description, version_no, is_system, schema_json, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6,
				$7, $8, 1, TRUE, $9::jsonb, NOW(), NOW()
			)
			RETURNING id
		`, tenantID, seed.Code, seed.Name, seed.ShortName, seed.DepartmentCode, seed.DepartmentName, seed.Status, seed.Description, string(schemaJSON)).Scan(&templateID); err != nil {
			return fmt.Errorf("insert builtin template: %w", err)
		}
		for _, binding := range seed.Bindings {
			ruleID, ruleSnapshot, err := s.resolveRuleForBinding(ctx, tenantID, binding)
			if err != nil {
				return fmt.Errorf("resolve builtin template binding %s: %w", binding.RuleCode, err)
			}
			stageJSON, err := json.Marshal(binding.StageConfigJSON)
			if err != nil {
				return fmt.Errorf("marshal builtin template binding stage: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO emr_template_rule_bindings (
					tenant_id, template_id, rule_id, rule_code, rule_name, rule_detail, enabled, rule_scope,
					stage_config_json, sort_order, created_at, updated_at
				) VALUES (
					$1, $2, $3, $4, $5, $6, $7, $8,
					$9::jsonb, $10, NOW(), NOW()
				)
			`, tenantID, templateID, ruleID, ruleSnapshot.Code, ruleSnapshot.Name, ruleSnapshot.Detail, binding.Enabled, binding.RuleScope, string(stageJSON), binding.SortOrder); err != nil {
				return fmt.Errorf("insert builtin template binding: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed emr templates: %w", err)
	}
	return nil
}

type bindingRuleSnapshot struct {
	Code   string
	Name   string
	Detail string
}

func (s *Store) EnsureMissingSeedBindings(ctx context.Context, tenantID int64) error {
	for _, seed := range builtinTemplates() {
		var templateID int64
		err := s.pool.QueryRow(ctx, `
			SELECT id
			FROM emr_templates
			WHERE tenant_id = $1
			  AND code = $2
			  AND deleted_at IS NULL
			ORDER BY id ASC
			LIMIT 1
		`, tenantID, seed.Code).Scan(&templateID)
		if err == pgx.ErrNoRows {
			continue
		}
		if err != nil {
			return fmt.Errorf("lookup seeded template %s: %w", seed.Code, err)
		}
		for _, binding := range seed.Bindings {
			ruleID, ruleSnapshot, err := s.resolveRuleForBinding(ctx, tenantID, binding)
			if err != nil {
				return fmt.Errorf("resolve missing template binding %s: %w", binding.RuleCode, err)
			}
			stageJSON, err := json.Marshal(binding.StageConfigJSON)
			if err != nil {
				return fmt.Errorf("marshal missing template binding stage: %w", err)
			}
			if _, err := s.pool.Exec(ctx, `
				INSERT INTO emr_template_rule_bindings (
					tenant_id, template_id, rule_id, rule_code, rule_name, rule_detail, enabled, rule_scope,
					stage_config_json, sort_order, created_at, updated_at
				)
				SELECT
					$1::bigint,
					$2::bigint,
					$3::text,
					$4::varchar(128),
					$5::text,
					$6::text,
					$7::boolean,
					$8::varchar(32),
					$9::jsonb,
					$10::integer,
					NOW(),
					NOW()
				WHERE NOT EXISTS (
					SELECT 1
					FROM emr_template_rule_bindings b
					WHERE b.template_id = $2::bigint
					  AND b.deleted_at IS NULL
					  AND (b.rule_id = $3::text OR b.rule_code = $4::varchar(128))
				)
			`, tenantID, templateID, ruleID, ruleSnapshot.Code, ruleSnapshot.Name, ruleSnapshot.Detail, binding.Enabled, binding.RuleScope, string(stageJSON), binding.SortOrder); err != nil {
				return fmt.Errorf("insert missing template binding %s: %w", binding.RuleCode, err)
			}
		}
	}
	return nil
}

func (s *Store) resolveRuleForBinding(ctx context.Context, tenantID int64, item BindingItemUpsert) (string, bindingRuleSnapshot, error) {
	ruleID := strings.TrimSpace(item.RuleID)
	ruleCode := strings.TrimSpace(item.RuleCode)
	if ruleID == "" && ruleCode == "" {
		return "", bindingRuleSnapshot{}, fmt.Errorf("rule_id or rule_code is required")
	}

	if ruleID != "" {
		var snapshot bindingRuleSnapshot
		if err := s.pool.QueryRow(ctx, `
			SELECT id, code, name, description
			FROM compliance_rules
			WHERE deleted_at IS NULL
			  AND id = $1
			  AND (tenant_id = 0 OR tenant_id = $2)
		`, ruleID, tenantID).Scan(&ruleID, &snapshot.Code, &snapshot.Name, &snapshot.Detail); err != nil {
			return "", bindingRuleSnapshot{}, fmt.Errorf("resolve rule_id %s: %w", ruleID, err)
		}
		return ruleID, snapshot, nil
	}

	var snapshot bindingRuleSnapshot
	if err := s.pool.QueryRow(ctx, `
		SELECT id, code, name, description
		FROM compliance_rules
		WHERE deleted_at IS NULL
		  AND code = $1
		  AND (tenant_id = 0 OR tenant_id = $2)
	`, ruleCode, tenantID).Scan(&ruleID, &snapshot.Code, &snapshot.Name, &snapshot.Detail); err != nil {
		return "", bindingRuleSnapshot{}, fmt.Errorf("resolve rule_code %s: %w", ruleCode, err)
	}
	return ruleID, snapshot, nil
}
