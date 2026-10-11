package complianceguard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool         *pgxpool.Pool
	llm          *llmgateway.Client
	processLogMu sync.Mutex
}

type FindingListFilter struct {
	TenantID    int64
	Status      []string
	Keyword     string
	SceneCode   string
	RoleCode    string
	Department  string
	EmployeeID  *int64
	RuleCode    string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Page        int
	PageSize    int
}

type FindingListResult struct {
	Items    []FindingListItem
	Total    int64
	Page     int
	PageSize int
}

type ComplianceRuleListFilter struct {
	Scope    string
	RoleCode string
	Enabled  *bool
}

var (
	errComplianceRuleNotFound = fmt.Errorf("compliance rule not found")
	errComplianceRuleConflict = fmt.Errorf("compliance rule conflict")
)

// ListRules 查询检查标准。
//
// 规则表是平台规则的唯一当前来源；接口返回完整 definition，同时把页面
// 常用字段单独返回，方便列表筛选和编辑。in_use 通过执行记录和发现记录判断。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) ListRules(ctx context.Context, filter ComplianceRuleListFilter) ([]ComplianceRule, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 2)
	next := 1
	if value := strings.TrimSpace(filter.Scope); value != "" {
		conditions = append(conditions, fmt.Sprintf("r.subject = $%d", next))
		args = append(args, value)
		next++
	}
	if value := strings.TrimSpace(filter.RoleCode); value != "" {
		conditions = append(conditions, fmt.Sprintf("r.role_codes @> jsonb_build_array($%d::text)", next))
		args = append(args, value)
		next++
	}
	if filter.Enabled != nil {
		conditions = append(conditions, fmt.Sprintf("r.is_enabled = $%d", next))
		args = append(args, *filter.Enabled)
		next++
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT r.id::text, r.rule_code, r.rule_name, r.subject, r.scene_codes,
		       r.role_codes, r.version, r.status, r.is_enabled, r.judgment_standard,
		       r.definition, r.created_by, r.created_at, r.updated_at,
		       EXISTS (
		         SELECT 1 FROM compliance_check_runs run
		         WHERE run.rule_version = r.version
		           AND run.subject = r.subject
		       ) OR EXISTS (
		         SELECT 1 FROM compliance_findings f
		         WHERE f.rule_code = r.rule_code
		           AND f.rule_version = r.version
		       ) AS in_use
		FROM compliance_rules r
		WHERE %s
		ORDER BY r.subject, r.rule_code, r.version DESC, r.updated_at DESC
	`, strings.Join(conditions, " AND ")), args...)
	if err != nil {
		return nil, fmt.Errorf("list compliance rules: %w", err)
	}
	defer rows.Close()

	items := make([]ComplianceRule, 0)
	for rows.Next() {
		item, err := scanComplianceRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate compliance rules: %w", err)
	}
	return items, nil
}

// GetRule 查询一条规则。
//
// 规则 ID 是版本级 ID，因此历史版本可以独立查看，不会被当前版本覆盖。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) GetRule(ctx context.Context, id string) (*ComplianceRule, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	var queryRow interface {
		Scan(dest ...any) error
	}
	queryRow = s.pool.QueryRow(ctx, `
		SELECT r.id::text, r.rule_code, r.rule_name, r.subject, r.scene_codes,
		       r.role_codes, r.version, r.status, r.is_enabled, r.judgment_standard,
		       r.definition, r.created_by, r.created_at, r.updated_at,
		       EXISTS (
		         SELECT 1 FROM compliance_check_runs run
		         WHERE run.rule_version = r.version AND run.subject = r.subject
		       ) OR EXISTS (
		         SELECT 1 FROM compliance_findings f
		         WHERE f.rule_code = r.rule_code AND f.rule_version = r.version
		       ) AS in_use
		FROM compliance_rules r
		WHERE r.id::text = $1
	`, id)
	item, err := scanComplianceRuleRow(queryRow)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, errComplianceRuleNotFound
		}
		return nil, fmt.Errorf("get compliance rule: %w", err)
	}
	return item, nil
}

// CreateRule 新增规则。
//
// 新规则默认发布且启用；版本缺省时由 API 生成。医院后续可以通过规则页面
// 再停用它，不需要删除数据库记录。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) CreateRule(ctx context.Context, actorID int64, req ComplianceRuleRequest) (*ComplianceRule, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	normalized, err := normalizeComplianceRuleRequest(req)
	if err != nil {
		return nil, err
	}
	definition := buildComplianceRuleDefinition(normalized)
	definitionJSON, _ := json.Marshal(definition)
	sceneCodes := normalized.SceneCodes
	roleCodes := normalized.RoleCodes
	version := normalized.Version
	if version == "" {
		version = newComplianceRuleVersion()
	}
	enabled := true
	if normalized.Enabled != nil {
		enabled = *normalized.Enabled
	}
	var id string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO compliance_rules (
		  rule_code, rule_name, subject, scene_codes, role_codes, version, status,
		  is_enabled, judgment_standard, definition, created_by, published_at, updated_at
		)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6, 'published', $7, $8, $9::jsonb,
		        $10, NOW(), NOW())
		RETURNING id::text
	`, normalized.Code, normalized.Name, normalized.Scope, sceneCodes, roleCodes, version,
		enabled, normalized.JudgmentStandard, definitionJSON, actorID).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: rule code and version already exist", errComplianceRuleConflict)
		}
		return nil, fmt.Errorf("create compliance rule: %w", err)
	}
	return s.GetRule(ctx, id)
}

// UpdateRule 修改规则；如果规则已被使用，则保留旧版本并插入新版本。
//
// 该逻辑保证历史检查结果中的 rule_version 永远有对应定义，避免编辑规则
// 造成历史结果含义漂移。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) UpdateRule(ctx context.Context, actorID int64, id string, req ComplianceRuleRequest) (*ComplianceRule, error) {
	current, err := s.GetRule(ctx, id)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeComplianceRuleRequest(req)
	if err != nil {
		return nil, err
	}
	definition := buildComplianceRuleDefinition(normalized)
	definitionJSON, _ := json.Marshal(definition)
	enabled := current.Enabled
	if normalized.Enabled != nil {
		enabled = *normalized.Enabled
	}
	if !current.InUse {
		_, err = s.pool.Exec(ctx, `
			UPDATE compliance_rules
			SET rule_code = $1, rule_name = $2, subject = $3,
			    scene_codes = $4::jsonb, role_codes = $5::jsonb,
			    is_enabled = $6, judgment_standard = $7, definition = $8::jsonb,
			    updated_at = NOW()
			WHERE id::text = $9
		`, normalized.Code, normalized.Name, normalized.Scope, normalized.SceneCodes,
			normalized.RoleCodes, enabled, normalized.JudgmentStandard, definitionJSON, id)
		if err != nil {
			if isUniqueViolation(err) {
				return nil, fmt.Errorf("%w: rule code and version already exist", errComplianceRuleConflict)
			}
			return nil, fmt.Errorf("update compliance rule: %w", err)
		}
		return s.GetRule(ctx, id)
	}

	version := normalized.Version
	if version == "" || version == current.Version {
		version = newComplianceRuleVersion()
	}
	var newID string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO compliance_rules (
		  rule_code, rule_name, subject, scene_codes, role_codes, version, status,
		  is_enabled, judgment_standard, definition, created_by, published_at, updated_at
		)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6, 'published', $7, $8, $9::jsonb,
		        $10, NOW(), NOW())
		RETURNING id::text
	`, normalized.Code, normalized.Name, normalized.Scope, normalized.SceneCodes,
		normalized.RoleCodes, version, enabled, normalized.JudgmentStandard, definitionJSON, actorID).Scan(&newID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: generated rule version already exists", errComplianceRuleConflict)
		}
		return nil, fmt.Errorf("create compliance rule version: %w", err)
	}
	return s.GetRule(ctx, newID)
}

func (s *Service) SetRuleEnabled(ctx context.Context, actorID int64, id string, enabled bool) (*ComplianceRule, error) {
	_, _ = actorID, enabled
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	var changedID string
	err := s.pool.QueryRow(ctx, `
		UPDATE compliance_rules
		SET is_enabled = $1, updated_at = NOW()
		WHERE id::text = $2
		RETURNING id::text
	`, enabled, id).Scan(&changedID)
	if err == pgx.ErrNoRows {
		return nil, errComplianceRuleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("set compliance rule enabled: %w", err)
	}
	return s.GetRule(ctx, changedID)
}

// DeleteRule 删除未被使用的规则。已使用规则只能停用或创建新版本。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) DeleteRule(ctx context.Context, actorID int64, id string) error {
	_, _ = actorID, id
	rule, err := s.GetRule(ctx, id)
	if err != nil {
		return err
	}
	if rule.InUse || rule.IsBuiltIn {
		return fmt.Errorf("%w: rule is already used or built in", errComplianceRuleConflict)
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM compliance_rules WHERE id::text = $1`, id)
	if err != nil {
		return fmt.Errorf("delete compliance rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errComplianceRuleNotFound
	}
	return nil
}

func scanComplianceRule(rows interface{ Scan(...any) error }) (*ComplianceRule, error) {
	return scanComplianceRuleRow(rows)
}

func scanComplianceRuleRow(row interface{ Scan(...any) error }) (*ComplianceRule, error) {
	var item ComplianceRule
	var sceneCodes, roleCodes, definition []byte
	var createdBy *int64
	if err := row.Scan(
		&item.ID, &item.Code, &item.Name, &item.Scope, &sceneCodes, &roleCodes,
		&item.Version, &item.Status, &item.Enabled, &item.JudgmentStandard,
		&definition, &createdBy, &item.CreatedAt, &item.UpdatedAt, &item.InUse,
	); err != nil {
		return nil, err
	}
	item.IsBuiltIn = createdBy == nil
	item.SceneCodes = json.RawMessage(defaultJSONArray(sceneCodes))
	item.RoleCodes = json.RawMessage(defaultJSONArray(roleCodes))
	item.Definition = json.RawMessage(defaultJSONObject(definition))
	item.Conditions, item.Examples = ruleDefinitionFields(item.Definition)
	item.Category = definitionString(item.Definition, "category")
	item.Description = definitionString(item.Definition, "description")
	item.LegalBasis = definitionString(item.Definition, "legal_basis")
	item.SuggestedScript = definitionString(item.Definition, "suggested_script")
	item.TriggerType = definitionString(item.Definition, "trigger_type")
	return &item, nil
}

func normalizeComplianceRuleRequest(req ComplianceRuleRequest) (ComplianceRuleRequest, error) {
	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	req.Scope = strings.TrimSpace(req.Scope)
	req.Category = strings.TrimSpace(req.Category)
	req.Severity = strings.TrimSpace(req.Severity)
	req.TriggerType = strings.TrimSpace(req.TriggerType)
	req.JudgmentStandard = strings.TrimSpace(req.JudgmentStandard)
	if req.Scope == "沟通合规" {
		req.Scope = "communication"
	}
	if req.Scope == "内容合规" {
		req.Scope = "content"
	}
	if req.Code == "" || req.Name == "" {
		return req, fmt.Errorf("code and name are required")
	}
	if req.Scope != "communication" && req.Scope != "content" {
		return req, fmt.Errorf("scope must be communication or content")
	}
	if req.Version != "" {
		req.Version = strings.TrimSpace(req.Version)
	}
	if len(req.SceneCodes) == 0 {
		if req.Scope == "communication" {
			req.SceneCodes = json.RawMessage(`["communication.doctor.outpatient","communication.consultant.consultation"]`)
		} else {
			req.SceneCodes = json.RawMessage(`["content.pre_publish"]`)
		}
	}
	if len(req.RoleCodes) == 0 {
		if req.Scope == "communication" {
			req.RoleCodes = json.RawMessage(`["doctor","consultant"]`)
		} else {
			req.RoleCodes = json.RawMessage(`[]`)
		}
	}
	return req, nil
}

func buildComplianceRuleDefinition(req ComplianceRuleRequest) map[string]any {
	definition := map[string]any{
		"rule_code":         req.Code,
		"name":              req.Name,
		"category":          req.Category,
		"subject":           req.Scope,
		"status":            "published",
		"judgment_standard": req.JudgmentStandard,
		"description":       req.Description,
		"legal_basis":       req.LegalBasis,
		"suggested_script":  req.SuggestedScript,
		"trigger_type":      req.TriggerType,
	}
	if len(req.Conditions) > 0 {
		var conditions any
		if json.Unmarshal(req.Conditions, &conditions) == nil {
			definition["conditions"] = conditions
		}
	}
	if len(req.Examples) > 0 {
		var examples any
		if json.Unmarshal(req.Examples, &examples) == nil {
			definition["examples"] = examples
		}
	}
	return definition
}

func ruleDefinitionFields(definition json.RawMessage) (json.RawMessage, json.RawMessage) {
	var object map[string]any
	if json.Unmarshal(definition, &object) != nil {
		return json.RawMessage(`{}`), json.RawMessage(`{}`)
	}
	conditions, _ := json.Marshal(object["conditions"])
	examples, _ := json.Marshal(object["examples"])
	if string(conditions) == "null" {
		conditions = []byte(`{}`)
	}
	if string(examples) == "null" {
		examples = []byte(`{}`)
	}
	return conditions, examples
}

func definitionString(definition json.RawMessage, key string) string {
	var object map[string]any
	if json.Unmarshal(definition, &object) != nil {
		return ""
	}
	value, _ := object[key].(string)
	return value
}

func defaultJSONArray(value []byte) []byte {
	if len(value) == 0 {
		return []byte(`[]`)
	}
	return value
}

func defaultJSONObject(value []byte) []byte {
	if len(value) == 0 {
		return []byte(`{}`)
	}
	return value
}

func newComplianceRuleVersion() string {
	return time.Now().Format("20060102.150405.000")
}

func isUniqueViolation(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "duplicate key") ||
		strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

func isComplianceRuleConflict(err error) bool {
	return strings.Contains(err.Error(), errComplianceRuleConflict.Error())
}

// GetOverview 聚合当前租户的合规发现。
//
// 维度统计使用发现表中的稳定字段；员工和科室只用于补齐展示名称。
// 规则名称本阶段不额外查询，避免依赖历史规则表的非统一字段结构。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) ListCheckItems(ctx context.Context, tenantID int64, status string) ([]CheckItemListItem, error) {
	query := `SELECT id::text,tenant_id,source_type,source_id,source_version,scene_code,role_code,employee_id,theme_key,question,risk_hypothesis,primary_risk_type,related_risk_types,evidence,check_basis,suggested_actions,priority,status,COALESCE(conclusion,''),COALESCE(process_reason,''),process_actions,COALESCE(process_note,''),created_at,updated_at FROM compliance_check_items WHERE tenant_id=$1`
	args := []any{tenantID}
	if strings.TrimSpace(status) != "" {
		query += " AND status=$2"
		args = append(args, strings.TrimSpace(status))
	}
	query += " ORDER BY created_at DESC"
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CheckItemListItem{}
	for rows.Next() {
		var i CheckItemListItem
		if err := rows.Scan(&i.ID, &i.TenantID, &i.SourceType, &i.SourceID, &i.SourceVersion, &i.SceneCode, &i.RoleCode, &i.EmployeeID, &i.ThemeKey, &i.Question, &i.RiskHypothesis, &i.PrimaryRiskType, &i.RelatedRiskTypes, &i.Evidence, &i.CheckBasis, &i.SuggestedActions, &i.Priority, &i.Status, &i.Conclusion, &i.ProcessReason, &i.ProcessActions, &i.ProcessNote, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
func (s *Service) GetCheckItem(ctx context.Context, tenantID int64, id string) (*CheckItemListItem, error) {
	var i CheckItemListItem
	err := s.pool.QueryRow(ctx, `SELECT id::text,tenant_id,source_type,source_id,source_version,scene_code,role_code,employee_id,theme_key,question,risk_hypothesis,primary_risk_type,related_risk_types,evidence,check_basis,suggested_actions,priority,status,COALESCE(conclusion,''),COALESCE(process_reason,''),process_actions,COALESCE(process_note,''),created_at,updated_at FROM compliance_check_items WHERE tenant_id=$1 AND id::text=$2`, tenantID, id).Scan(&i.ID, &i.TenantID, &i.SourceType, &i.SourceID, &i.SourceVersion, &i.SceneCode, &i.RoleCode, &i.EmployeeID, &i.ThemeKey, &i.Question, &i.RiskHypothesis, &i.PrimaryRiskType, &i.RelatedRiskTypes, &i.Evidence, &i.CheckBasis, &i.SuggestedActions, &i.Priority, &i.Status, &i.Conclusion, &i.ProcessReason, &i.ProcessActions, &i.ProcessNote, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text,actor_id,conclusion,reason,actions,note,created_at FROM compliance_check_item_process_records WHERE tenant_id=$1 AND item_id=$2::uuid ORDER BY created_at ASC`, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var record CheckItemProcessRecord
		if err := rows.Scan(&record.ID, &record.ActorID, &record.Conclusion, &record.Reason, &record.Actions, &record.Note, &record.CreatedAt); err != nil {
			return nil, err
		}
		i.ProcessRecords = append(i.ProcessRecords, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &i, nil
}
func (s *Service) StartCheckItem(ctx context.Context, tenantID int64, id string, actor int64) (*CheckItemListItem, error) {
	result, err := s.pool.Exec(ctx, `UPDATE compliance_check_items SET status=CASE WHEN status='pending' THEN 'in_progress' ELSE status END, started_by=CASE WHEN started_by IS NULL THEN $3 ELSE started_by END, started_at=CASE WHEN started_at IS NULL THEN NOW() ELSE started_at END, updated_at=NOW() WHERE tenant_id=$1 AND id::text=$2`, tenantID, id, actor)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		return nil, pgx.ErrNoRows
	}
	return s.GetCheckItem(ctx, tenantID, id)
}
func (s *Service) ProcessCheckItem(ctx context.Context, tenantID int64, id string, actor int64, req CheckItemProcessRequest) (*CheckItemListItem, error) {
	validConclusion := map[string]bool{"expressions_need_improvement": true, "medical_basis_needed": true, "disclosure_boundary_needed": true, "management_risk_confirmed": true, "insufficient_evidence": true, "refer_to_other_department": true}
	if !validConclusion[strings.TrimSpace(req.Conclusion)] || strings.TrimSpace(req.Reason) == "" || len(req.Actions) == 0 || strings.TrimSpace(req.IdempotencyKey) == "" {
		return nil, fmt.Errorf("conclusion, reason and actions are required")
	}
	b, _ := json.Marshal(req.Actions)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM compliance_check_items WHERE tenant_id=$1 AND id::text=$2 FOR UPDATE`, tenantID, id).Scan(&status); err != nil {
		return nil, err
	}
	if status == "processed" {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			return nil, err
		}
		return s.GetCheckItem(ctx, tenantID, id)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO compliance_check_item_process_records(item_id,tenant_id,actor_id,idempotency_key,conclusion,reason,actions,note) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8)`, id, tenantID, actor, req.IdempotencyKey, req.Conclusion, req.Reason, string(b), req.Note); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE compliance_check_items SET status='processed',processed_by=$3,processed_at=NOW(),conclusion=$4,process_reason=$5,process_actions=$6::jsonb,process_note=$7,updated_at=NOW() WHERE tenant_id=$1 AND id::text=$2`, tenantID, id, actor, req.Conclusion, req.Reason, string(b), req.Note); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetCheckItem(ctx, tenantID, id)
}

func (s *Service) GetOverview(ctx context.Context, tenantID int64) (*ComplianceOverview, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	if tenantID <= 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}

	overview := &ComplianceOverview{
		ByScene:         make([]OverviewBucket, 0),
		ByDepartment:    make([]OverviewBucket, 0),
		ByEmployee:      make([]OverviewBucket, 0),
		ByRule:          make([]OverviewBucket, 0),
		RecentPending:   make([]CheckItemListItem, 0),
		RecentReviewed:  make([]CheckItemListItem, 0),
		RecentConfirmed: make([]CheckItemListItem, 0),
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE f.status IN ('pending','in_progress')),
		  COUNT(*) FILTER (WHERE f.status = 'processed'),
		  COUNT(*) FILTER (WHERE f.status = 'processed' AND f.conclusion IN ('management_risk_confirmed','medical_basis_needed'))
		FROM compliance_check_items f
		WHERE f.tenant_id = $1
	`, tenantID).Scan(
		&overview.PendingCount,
		&overview.ReviewedCount,
		&overview.ConfirmedCount,
	); err != nil {
		return nil, fmt.Errorf("count compliance overview: %w", err)
	}

	var err error
	overview.ByScene, err = s.loadOverviewBuckets(ctx, `
		SELECT f.scene_code, f.scene_code, COUNT(*)
		FROM compliance_check_items f
		WHERE f.tenant_id = $1
		GROUP BY f.scene_code
		ORDER BY COUNT(*) DESC, f.scene_code
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load compliance overview scenes: %w", err)
	}
	overview.ByDepartment, err = s.loadOverviewBuckets(ctx, `
		SELECT COALESCE(d.id::text, ''), COALESCE(NULLIF(d.name, ''), '未记录科室'), COUNT(*)
		FROM compliance_check_items f
		LEFT JOIN employees e
		  ON e.id = f.employee_id AND e.tenant_id = f.tenant_id AND e.deleted_at IS NULL
		LEFT JOIN departments d
		  ON d.id = e.department_id AND d.tenant_id = f.tenant_id AND d.deleted_at IS NULL
		WHERE f.tenant_id = $1
		GROUP BY d.id, d.name
		ORDER BY COUNT(*) DESC, COALESCE(NULLIF(d.name, ''), '未记录科室')
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load compliance overview departments: %w", err)
	}
	overview.ByEmployee, err = s.loadOverviewBuckets(ctx, `
		SELECT COALESCE(f.employee_id::text, ''), COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未记录人员'), COUNT(*)
		FROM compliance_check_items f
		LEFT JOIN employees e
		  ON e.id = f.employee_id AND e.tenant_id = f.tenant_id AND e.deleted_at IS NULL
		WHERE f.tenant_id = $1
		GROUP BY f.employee_id, e.full_name, e.name
		ORDER BY COUNT(*) DESC, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未记录人员')
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load compliance overview employees: %w", err)
	}
	overview.ByRule, err = s.loadOverviewBuckets(ctx, `
		SELECT src.rule_code, COALESCE(NULLIF(r.rule_name, ''), '其他问题'), COUNT(DISTINCT i.id)
		FROM compliance_check_items i
		JOIN compliance_check_item_sources src ON src.item_id = i.id
		LEFT JOIN LATERAL (
		    SELECT rule_name
		    FROM compliance_rules
			WHERE compliance_rules.rule_code = src.rule_code
		      AND compliance_rules.status = 'published'
		      AND (
				true
		      )
		    ORDER BY compliance_rules.updated_at DESC, compliance_rules.id DESC
		    LIMIT 1
		) r ON true
		WHERE i.tenant_id = $1
		GROUP BY src.rule_code, r.rule_name
		ORDER BY COUNT(DISTINCT i.id) DESC, COALESCE(NULLIF(r.rule_name, ''), '其他问题')
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load compliance overview rules: %w", err)
	}
	overview.RecentPending, err = s.loadRecentCheckItems(ctx, tenantID, "pending")
	if err != nil {
		return nil, fmt.Errorf("load recent compliance findings: %w", err)
	}
	overview.RecentReviewed, err = s.loadRecentCheckItems(ctx, tenantID, "processed")
	if err != nil {
		return nil, fmt.Errorf("load recent reviewed compliance findings: %w", err)
	}
	overview.RecentConfirmed, err = s.loadRecentCheckItems(ctx, tenantID, "confirmed")
	if err != nil {
		return nil, fmt.Errorf("load recent confirmed compliance findings: %w", err)
	}
	return overview, nil
}

func (s *Service) loadOverviewBuckets(ctx context.Context, query string, tenantID int64) ([]OverviewBucket, error) {
	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OverviewBucket, 0)
	for rows.Next() {
		var item OverviewBucket
		if err := rows.Scan(&item.Key, &item.Label, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) loadRecentPendingFindings(ctx context.Context, tenantID int64) ([]FindingListItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id::text, f.tenant_id, f.source_type, f.source_id, f.source_version,
		       f.subject, f.scene_code, f.role_code, f.employee_id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '') AS employee_name,
		       COALESCE(NULLIF(c.name, ''), '') AS customer_name,
		       COALESCE(d.name, '') AS department,
		       f.rule_code, f.rule_code AS rule_name,
		       f.rule_version, f.fact, f.summary, f.needs_review, f.status,
		       f.review_conclusion, f.reviewed_at, f.created_at, f.updated_at
		FROM compliance_findings f
		LEFT JOIN recordings rec
		  ON rec.id::text = f.source_id
		 AND rec.tenant_id = f.tenant_id
		 AND f.source_type = 'recording'
		LEFT JOIN customers c
		  ON c.id = rec.customer_id AND c.tenant_id = f.tenant_id
		LEFT JOIN employees e
		  ON e.id = f.employee_id AND e.tenant_id = f.tenant_id AND e.deleted_at IS NULL
		LEFT JOIN departments d
		  ON d.id = e.department_id AND d.tenant_id = f.tenant_id AND d.deleted_at IS NULL
		WHERE f.tenant_id = $1 AND f.status = 'pending'
		ORDER BY f.created_at DESC, f.id DESC
		LIMIT 5
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]FindingListItem, 0, 5)
	for rows.Next() {
		var item FindingListItem
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.SourceType, &item.SourceID, &item.SourceVersion,
			&item.Subject, &item.SceneCode, &item.RoleCode, &item.EmployeeID,
			&item.EmployeeName, &item.CustomerName, &item.Department, &item.RuleCode, &item.RuleName,
			&item.RuleVersion, &item.Fact, &item.Summary, &item.NeedsReview,
			&item.Status, &item.ReviewConclusion, &item.ReviewedAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) loadRecentFindings(ctx context.Context, tenantID int64, confirmedOnly bool) ([]FindingListItem, error) {
	statusCondition := "f.status IN ('confirmed', 'not_confirmed', 'unable_to_determine')"
	orderColumn := "f.reviewed_at"
	if confirmedOnly {
		statusCondition = "f.status = 'confirmed'"
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT f.id::text, f.tenant_id, f.source_type, f.source_id, f.source_version,
		       f.subject, f.scene_code, f.role_code, f.employee_id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '') AS employee_name,
		       COALESCE(NULLIF(c.name, ''), '') AS customer_name,
		       COALESCE(d.name, '') AS department,
		       f.rule_code, COALESCE(NULLIF(r.rule_name, ''), '其他问题') AS rule_name,
		       f.rule_version, f.fact, f.summary, f.needs_review, f.status,
		       f.review_conclusion, f.reviewed_at, f.created_at, f.updated_at
		FROM compliance_findings f
		LEFT JOIN recordings rec
		  ON rec.id::text = f.source_id
		 AND rec.tenant_id = f.tenant_id
		 AND f.source_type = 'recording'
		LEFT JOIN customers c
		  ON c.id = rec.customer_id AND c.tenant_id = f.tenant_id
		LEFT JOIN employees e
		  ON e.id = f.employee_id AND e.tenant_id = f.tenant_id AND e.deleted_at IS NULL
		LEFT JOIN departments d
		  ON d.id = e.department_id AND d.tenant_id = f.tenant_id AND d.deleted_at IS NULL
		LEFT JOIN LATERAL (
		    SELECT rule_name
		    FROM compliance_rules
		    WHERE compliance_rules.rule_code = f.rule_code
		      AND compliance_rules.status = 'published'
		      AND (
		          NULLIF(f.rule_version, '') IS NULL
		          OR compliance_rules.version = f.rule_version
		      )
		    ORDER BY compliance_rules.updated_at DESC, compliance_rules.id DESC
		    LIMIT 1
		) r ON true
		WHERE f.tenant_id = $1 AND %s
		ORDER BY %s DESC NULLS LAST, f.id DESC
		LIMIT 5
	`, statusCondition, orderColumn), tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]FindingListItem, 0, 5)
	for rows.Next() {
		var item FindingListItem
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.SourceType, &item.SourceID, &item.SourceVersion,
			&item.Subject, &item.SceneCode, &item.RoleCode, &item.EmployeeID,
			&item.EmployeeName, &item.CustomerName, &item.Department, &item.RuleCode, &item.RuleName,
			&item.RuleVersion, &item.Fact, &item.Summary, &item.NeedsReview, &item.Status,
			&item.ReviewConclusion, &item.ReviewedAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) loadRecentCheckItems(ctx context.Context, tenantID int64, status string) ([]CheckItemListItem, error) {
	condition, args := recentCheckItemsCondition(tenantID, status)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT i.id::text,i.tenant_id,i.source_type,i.source_id,i.source_version,i.scene_code,i.role_code,i.employee_id,i.theme_key,i.question,i.risk_hypothesis,i.primary_risk_type,i.related_risk_types,i.evidence,i.check_basis,i.suggested_actions,i.priority,i.status,COALESCE(i.conclusion,''),COALESCE(i.process_reason,''),i.process_actions,COALESCE(i.process_note,''),i.created_at,i.updated_at FROM compliance_check_items i WHERE i.tenant_id=$1 AND %s ORDER BY i.updated_at DESC,i.id DESC LIMIT 5`, condition), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CheckItemListItem, 0, 5)
	for rows.Next() {
		var i CheckItemListItem
		if err := rows.Scan(&i.ID, &i.TenantID, &i.SourceType, &i.SourceID, &i.SourceVersion, &i.SceneCode, &i.RoleCode, &i.EmployeeID, &i.ThemeKey, &i.Question, &i.RiskHypothesis, &i.PrimaryRiskType, &i.RelatedRiskTypes, &i.Evidence, &i.CheckBasis, &i.SuggestedActions, &i.Priority, &i.Status, &i.Conclusion, &i.ProcessReason, &i.ProcessActions, &i.ProcessNote, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func recentCheckItemsCondition(tenantID int64, status string) (string, []any) {
	if status == "confirmed" {
		return "i.status = 'processed' AND i.conclusion = 'management_risk_confirmed'", []any{tenantID}
	}
	return "i.status = $2", []any{tenantID, status}
}

var (
	errFindingNotFound        = fmt.Errorf("compliance finding not found")
	errFindingAlreadyReviewed = fmt.Errorf("compliance finding has already been reviewed")
)

// ListFindings 查询用户可见的合规发现。
//
// 这里只读 compliance_findings；没有发现的执行记录不进入列表。
// 员工和科室名称通过现有主数据表补齐，查询始终带租户条件。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) ListFindings(ctx context.Context, filter FindingListFilter) (*FindingListResult, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	if filter.TenantID <= 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}

	conditions, args := findingWhereClause(filter)
	where := strings.Join(conditions, " AND ")
	var total int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM compliance_findings f
		LEFT JOIN recordings rec
		  ON rec.id::text = f.source_id
		 AND rec.tenant_id = f.tenant_id
		 AND f.source_type = 'recording'
		LEFT JOIN customers c
		  ON c.id = rec.customer_id AND c.tenant_id = f.tenant_id
		LEFT JOIN employees e
		  ON e.id = f.employee_id AND e.tenant_id = f.tenant_id AND e.deleted_at IS NULL
		LEFT JOIN departments d
		  ON d.id = e.department_id AND d.tenant_id = f.tenant_id AND d.deleted_at IS NULL
		WHERE `+where, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count compliance findings: %w", err)
	}

	queryArgs := append([]any{}, args...)
	limitArg := len(queryArgs) + 1
	offsetArg := len(queryArgs) + 2
	queryArgs = append(queryArgs, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT f.id::text, f.tenant_id, f.source_type, f.source_id, f.source_version,
		       f.subject, f.scene_code, f.role_code, f.employee_id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '') AS employee_name,
		       COALESCE(NULLIF(c.name, ''), '') AS customer_name,
		       COALESCE(d.name, '') AS department,
		       f.rule_code, COALESCE(r.rule_name, f.rule_code) AS rule_name,
		       f.rule_version, f.fact, f.summary, f.needs_review, f.status,
		       f.created_at, f.updated_at
		FROM compliance_findings f
		LEFT JOIN recordings rec
		  ON rec.id::text = f.source_id
		 AND rec.tenant_id = f.tenant_id
		 AND f.source_type = 'recording'
		LEFT JOIN customers c
		  ON c.id = rec.customer_id AND c.tenant_id = f.tenant_id
		LEFT JOIN employees e
		  ON e.id = f.employee_id AND e.tenant_id = f.tenant_id AND e.deleted_at IS NULL
		LEFT JOIN departments d
		  ON d.id = e.department_id AND d.tenant_id = f.tenant_id AND d.deleted_at IS NULL
		LEFT JOIN LATERAL (
		    SELECT rule_name
		    FROM compliance_rules
		    WHERE compliance_rules.rule_code = f.rule_code
		      AND compliance_rules.status = 'published'
		      AND compliance_rules.is_enabled = true
		      AND (
		          NULLIF(f.rule_version, '') IS NULL
		          OR compliance_rules.version = f.rule_version
		      )
		    ORDER BY compliance_rules.updated_at DESC, compliance_rules.id DESC
		    LIMIT 1
		) r ON true
		WHERE %s
		ORDER BY f.created_at DESC, f.id DESC
		LIMIT $%d OFFSET $%d
	`, where, limitArg, offsetArg), queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("list compliance findings: %w", err)
	}
	defer rows.Close()

	items := make([]FindingListItem, 0, filter.PageSize)
	for rows.Next() {
		var item FindingListItem
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.SourceType, &item.SourceID, &item.SourceVersion,
			&item.Subject, &item.SceneCode, &item.RoleCode, &item.EmployeeID,
			&item.EmployeeName, &item.CustomerName, &item.Department, &item.RuleCode, &item.RuleName,
			&item.RuleVersion, &item.Fact, &item.Summary, &item.NeedsReview,
			&item.Status, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan compliance finding: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate compliance findings: %w", err)
	}
	return &FindingListResult{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func findingWhereClause(filter FindingListFilter) ([]string, []any) {
	conditions := []string{"f.tenant_id = $1"}
	args := []any{filter.TenantID}
	next := 2
	if len(filter.Status) > 0 {
		placeholders := make([]string, 0, len(filter.Status))
		for _, status := range filter.Status {
			status = strings.TrimSpace(status)
			if status == "" {
				continue
			}
			placeholders = append(placeholders, fmt.Sprintf("$%d", next))
			args = append(args, status)
			next++
		}
		if len(placeholders) > 0 {
			conditions = append(conditions, "f.status IN ("+strings.Join(placeholders, ", ")+")")
		}
	}
	if value := strings.TrimSpace(filter.SceneCode); value != "" {
		conditions = append(conditions, fmt.Sprintf("f.scene_code = $%d", next))
		args = append(args, value)
		next++
	}
	if value := strings.TrimSpace(filter.RoleCode); value != "" {
		conditions = append(conditions, fmt.Sprintf("f.role_code = $%d", next))
		args = append(args, value)
		next++
	}
	if value := strings.TrimSpace(filter.Department); value != "" {
		conditions = append(conditions, fmt.Sprintf("d.name ILIKE $%d", next))
		args = append(args, "%"+value+"%")
		next++
	}
	if value := strings.TrimSpace(filter.Keyword); value != "" {
		conditions = append(conditions, fmt.Sprintf(`(
			COALESCE(e.full_name, '') ILIKE $%d OR
			COALESCE(e.name, '') ILIKE $%d OR
			COALESCE(c.name, '') ILIKE $%d OR
			COALESCE(f.summary, '') ILIKE $%d OR
			COALESCE(f.fact, '') ILIKE $%d OR
			COALESCE(f.evidence::text, '') ILIKE $%d OR
			f.source_id ILIKE $%d
		)`, next, next, next, next, next, next, next))
		args = append(args, "%"+value+"%")
		next++
	}
	if filter.EmployeeID != nil {
		conditions = append(conditions, fmt.Sprintf("f.employee_id = $%d", next))
		args = append(args, *filter.EmployeeID)
		next++
	}
	if value := strings.TrimSpace(filter.RuleCode); value != "" {
		conditions = append(conditions, fmt.Sprintf("f.rule_code = $%d", next))
		args = append(args, value)
		next++
	}
	if filter.CreatedFrom != nil {
		conditions = append(conditions, fmt.Sprintf("f.created_at >= $%d", next))
		args = append(args, *filter.CreatedFrom)
		next++
	}
	if filter.CreatedTo != nil {
		conditions = append(conditions, fmt.Sprintf("f.created_at < $%d", next))
		args = append(args, *filter.CreatedTo)
	}
	return conditions, args
}

// GetFinding 返回单条发现及其证据、核查字段和当前规则判断标准。
//
// 详情查询仍以发现表为主，不把执行记录当作用户业务对象。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) GetFinding(ctx context.Context, tenantID int64, id string) (*FindingDetail, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	var item FindingDetail
	err := s.pool.QueryRow(ctx, `
		SELECT f.id::text, f.tenant_id, f.source_type, f.source_id, f.source_version,
		       f.subject, f.scene_code, f.role_code, f.employee_id,
		       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '') AS employee_name,
		       COALESCE(NULLIF(c.name, ''), '') AS customer_name,
		       COALESCE(d.name, '') AS department,
		       f.rule_code, COALESCE(r.rule_name, f.rule_code) AS rule_name,
		       f.rule_version, f.fact, f.summary, f.needs_review, f.status,
		       f.created_at, f.updated_at, f.reason, f.evidence, f.missing_facts,
		       f.review_conclusion, f.review_reason, f.reviewed_by, f.reviewed_at,
		       COALESCE(r.judgment_standard, '')
		FROM compliance_findings f
		LEFT JOIN recordings rec
		  ON rec.id::text = f.source_id
		 AND rec.tenant_id = f.tenant_id
		 AND f.source_type = 'recording'
		LEFT JOIN customers c
		  ON c.id = rec.customer_id AND c.tenant_id = f.tenant_id
		LEFT JOIN employees e
		  ON e.id = f.employee_id AND e.tenant_id = f.tenant_id AND e.deleted_at IS NULL
		LEFT JOIN departments d
		  ON d.id = e.department_id AND d.tenant_id = f.tenant_id AND d.deleted_at IS NULL
		LEFT JOIN LATERAL (
		    SELECT rule_name, judgment_standard
		    FROM compliance_rules
		    WHERE compliance_rules.rule_code = f.rule_code
		      AND compliance_rules.status = 'published'
		      AND compliance_rules.is_enabled = true
		      AND (
		          NULLIF(f.rule_version, '') IS NULL
		          OR compliance_rules.version = f.rule_version
		      )
		    ORDER BY compliance_rules.updated_at DESC, compliance_rules.id DESC
		    LIMIT 1
		) r ON true
		WHERE f.id::text = $1 AND f.tenant_id = $2
	`, id, tenantID).Scan(
		&item.ID, &item.TenantID, &item.SourceType, &item.SourceID, &item.SourceVersion,
		&item.Subject, &item.SceneCode, &item.RoleCode, &item.EmployeeID,
		&item.EmployeeName, &item.CustomerName, &item.Department, &item.RuleCode, &item.RuleName,
		&item.RuleVersion, &item.Fact, &item.Summary, &item.NeedsReview, &item.Status,
		&item.CreatedAt, &item.UpdatedAt, &item.Reason, &item.Evidence, &item.MissingFacts,
		&item.ReviewConclusion, &item.ReviewReason, &item.ReviewedBy, &item.ReviewedAt,
		&item.Rule.JudgmentStandard,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("get compliance finding: %w", err)
	}
	item.Rule.Code = item.RuleCode
	item.Rule.Name = item.RuleName
	item.Rule.Version = item.RuleVersion
	item.Source = FindingSource{
		Type: item.SourceType, ID: item.SourceID, Version: item.SourceVersion,
		Subject: item.Subject, SceneCode: item.SceneCode, RoleCode: item.RoleCode,
		EmployeeID: item.EmployeeID, EmployeeName: item.EmployeeName, Department: item.Department,
	}
	return &item, nil
}

// ReviewFinding 将一次人工核查和对应审计日志放在同一个事务中提交。
//
// 发现只能从 pending 进入最终核查状态；如果已经核查过，返回冲突而不是
// 覆盖历史结论。system_action_logs 是现有系统操作日志表，本阶段不新增审计表。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) ReviewFinding(ctx context.Context, tenantID int64, id string, actorID int64, actorType string, req ReviewFindingRequest, request *http.Request) (*ReviewFindingResponse, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}
	if tenantID <= 0 || actorID <= 0 {
		return nil, fmt.Errorf("review actor is invalid")
	}
	if err := validateReviewFindingRequest(req); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin review finding: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var result ReviewFindingResponse
	err = tx.QueryRow(ctx, `
		UPDATE compliance_findings
		SET status = $1,
		    review_conclusion = $2,
		    review_reason = $3,
		    reviewed_by = $4,
		    reviewed_at = NOW(),
		    updated_at = NOW()
		WHERE id::text = $5
		  AND tenant_id = $6
		  AND status = 'pending'
		RETURNING id::text, tenant_id, status, review_conclusion, review_reason,
		          reviewed_by, reviewed_at
	`, strings.TrimSpace(req.ReviewConclusion), strings.TrimSpace(req.ReviewConclusion),
		strings.TrimSpace(req.ReviewReason), actorID, id, tenantID).Scan(
		&result.ID, &result.TenantID, &result.Status, &result.ReviewConclusion,
		&result.ReviewReason, &result.ReviewedBy, &result.ReviewedAt,
	)
	if err != nil {
		if err != pgx.ErrNoRows {
			return nil, fmt.Errorf("update compliance finding review: %w", err)
		}
		var currentStatus string
		err = tx.QueryRow(ctx, `
			SELECT status
			FROM compliance_findings
			WHERE id::text = $1 AND tenant_id = $2
		`, id, tenantID).Scan(&currentStatus)
		if err == pgx.ErrNoRows {
			return nil, errFindingNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("check compliance finding review status: %w", err)
		}
		if currentStatus != "pending" {
			return nil, errFindingAlreadyReviewed
		}
		return nil, fmt.Errorf("review finding update affected no rows")
	}

	requestSummary, _ := json.Marshal(map[string]any{
		"review_conclusion": result.ReviewConclusion,
		"review_reason":     result.ReviewReason,
	})
	afterSummary, _ := json.Marshal(map[string]any{
		"status":            result.Status,
		"review_conclusion": result.ReviewConclusion,
		"review_reason":     result.ReviewReason,
		"reviewed_by":       result.ReviewedBy,
		"reviewed_at":       result.ReviewedAt,
	})
	var ipAddress, userAgent string
	if request != nil {
		ipAddress = request.RemoteAddr
		userAgent = request.UserAgent()
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO system_action_logs (
			tenant_id, actor_id, actor_role_code, log_type, action_code, action_name,
			result, object_type, object_id, request_summary, after_summary,
			ip_address, user_agent, request_id, created_at
		)
		VALUES ($1, $2, $3, 'compliance', 'compliance_finding_review', '核查合规发现',
		        'success', 'compliance_finding', $4, $5::jsonb, $6::jsonb,
		        $7, $8, $9, NOW())
	`, tenantID, actorID, strings.TrimSpace(actorType), result.ID, string(requestSummary),
		string(afterSummary), ipAddress, userAgent, request.Header.Get("X-Request-Id"))
	if err != nil {
		return nil, fmt.Errorf("write compliance review audit log: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit compliance finding review: %w", err)
	}
	return &result, nil
}

type complianceModelConfig struct {
	ID           int64
	TenantID     int64
	FunctionType string
	Provider     string
	ModelCode    string
	ModelParams  map[string]any
}

const (
	complianceDataDir          = "/Users/yiliiang/Documents/lingce-web/apps/compliance/data"
	complianceProcessLogDir    = complianceDataDir + "/log"
	compliancePromptCode       = "compliance_check_v1"
	complianceDoctorPromptCode = "compliance_doctor_check_v1"
)

// InvalidReferenceError 表示 Worker 请求中的业务引用无法校验。
//
// 合规模块独立定义该错误，不依赖随访模块的业务类型。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
type InvalidReferenceError struct {
	Message string
}

func (e *InvalidReferenceError) Error() string {
	return e.Message
}

// UnsupportedRoleError 表示当前沟通合规接口尚未定义该员工角色的规则集。
//
// 角色不支持时必须显式返回，不能静默套用医生或咨询师规则，避免产生
// 无业务依据的合规判断。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-13
type UnsupportedRoleError struct {
	Message string
}

func (e *UnsupportedRoleError) Error() string {
	return e.Message
}

func NewService(pool *pgxpool.Pool, llm *llmgateway.Client) *Service {
	return &Service{pool: pool, llm: llm}
}

// AcceptCommunicationCheck 是 Worker 调用合规卫士时的正式沟通检查入口。
//
// 本阶段同步完成一次 LLM 检查并将结果写入合规检查结果表。人工复核仍属于后续
// 业务阶段；过程日志由 Handler 统一负责从“收到请求”记录到“返回响应”结束。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-12
func (s *Service) AcceptCommunicationCheck(ctx context.Context, req CommunicationCheckRequest) (*CommunicationCheckResponse, *communicationRuntime, error) {
	if err := validateCommunicationRole(req.BusinessScope, req.RoleCode); err != nil {
		return nil, nil, err
	}
	runtime, err := s.buildCommunicationRuntime(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	modelConfig, err := s.loadModelConfig(ctx, req.TenantID)
	if err != nil {
		return nil, runtime, err
	}
	runtime.ModelConfigID = modelConfig.ID
	runtime.ModelTenantID = modelConfig.TenantID
	runtime.ModelFunctionType = modelConfig.FunctionType
	runtime.Provider = modelConfig.Provider
	runtime.ModelCode = modelConfig.ModelCode
	runtime.ModelParamsJSON = mustMarshalIndent(modelConfig.ModelParams)

	inputFingerprint := communicationInputFingerprint(req)
	executionKey := buildCommunicationExecutionKey(req, runtime, modelConfig.ID, inputFingerprint)
	existing, claimed, err := s.claimCommunicationRun(ctx, req, runtime, modelConfig, inputFingerprint, executionKey)
	if err != nil {
		return nil, runtime, err
	}
	if !claimed {
		return existing, runtime, nil
	}

	requestID := existing.RunID
	llmRequest := buildCommunicationLLMRequest(req.TenantID, fmt.Sprintf("%d", req.RecordingID), runtime.RuleSetCode, runtime.RuleSetCode, runtime.RuleSetVersion, runtime.RuleSetName, modelConfig, runtime)
	llmResponse, requestJSON, responseJSON, err := s.llm.TextInferenceWithRaw(ctx, llmRequest)
	runtime.GatewayRequestJSON = string(requestJSON)
	runtime.GatewayResponseJSON = string(responseJSON)
	if err != nil {
		runtime.GatewayError = err.Error()
		if persistErr := s.failCommunicationRun(ctx, requestID, runtime, inputFingerprint, executionKey, err); persistErr != nil {
			return nil, runtime, persistErr
		}
		return nil, runtime, err
	}
	runtime.FormattedResult = formatJSONText(llmResponse.Content)
	findings, err := parseFindingsForRuntime(llmResponse.Content, req.CleanedTranscript, runtimeRules(runtime), runtime.OutputContract)
	if err != nil {
		runtime.GatewayError = err.Error()
		if persistErr := s.failCommunicationRun(ctx, requestID, runtime, inputFingerprint, executionKey, err); persistErr != nil {
			return nil, runtime, persistErr
		}
		return nil, runtime, err
	}

	response := &CommunicationCheckResponse{
		Code:           "COMPLETED",
		Status:         "completed",
		Message:        "communication check completed",
		RequestID:      requestID,
		RunID:          requestID,
		TenantID:       req.TenantID,
		RecordingID:    req.RecordingID,
		EncounterID:    req.EncounterID,
		EmployeeID:     req.EmployeeID,
		CustomerID:     req.CustomerID,
		RuleSetCode:    runtime.RuleSetCode,
		RuleSetVersion: runtime.RuleSetVersion,
		PromptCode:     runtime.PromptCode,
		PromptVersion:  runtime.PromptVersion,
		ModelConfigID:  modelConfig.ID,
		ModelCode:      modelConfig.ModelCode,
		Findings:       findings,
		RawResponse:    llmResponse.Content,
	}
	if err := s.completeCommunicationRun(ctx, response, runtime, inputFingerprint, executionKey); err != nil {
		return nil, runtime, err
	}
	return response, runtime, nil
}

type storedCommunicationResult struct {
	RunID          string
	Status         string
	TenantID       int64
	SourceID       string
	SourceVersion  string
	RuleSetCode    string
	RuleSetVersion string
	RoleCode       string
	EmployeeID     int64
	PromptCode     string
	PromptVersion  string
	ModelConfigID  int64
	ModelCode      string
	FindingCount   int
	RawResponse    []byte
	ResultJSON     []byte
	ErrorMessage   string
}

// communicationInputFingerprint 对 Worker 提交的业务输入做稳定摘要。
//
// 摘要只包含影响分析结果的字段，避免同一份录音因请求时间或显示名称变化
// 被重复调用模型。结果表使用该摘要和规则、提示词、模型版本共同形成幂等键。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func communicationInputFingerprint(req CommunicationCheckRequest) string {
	payload := struct {
		TenantID          int64
		RecordingID       int64
		EncounterID       int64
		EmployeeID        int64
		CustomerID        int64
		BusinessScope     string
		RoleCode          string
		RecordedAt        string
		CleanedTranscript string
	}{
		TenantID:          req.TenantID,
		RecordingID:       req.RecordingID,
		EncounterID:       req.EncounterID,
		EmployeeID:        req.EmployeeID,
		CustomerID:        req.CustomerID,
		BusinessScope:     strings.TrimSpace(req.BusinessScope),
		RoleCode:          strings.TrimSpace(req.RoleCode),
		RecordedAt:        strings.TrimSpace(req.RecordedAt),
		CleanedTranscript: req.CleanedTranscript,
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// buildCommunicationExecutionKey 把输入、规则、提示词和模型配置绑定为一次
// 可重放但不可重复执行的分析身份。
//
// 规则或提示词版本变化后会形成新的执行键，因此优化配置不会错误复用旧结果。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func buildCommunicationExecutionKey(req CommunicationCheckRequest, runtime *communicationRuntime, modelConfigID int64, inputFingerprint string) string {
	key := fmt.Sprintf(
		"communication:%d:recording:%d:%s:%s:%s:%s:%s:%d:%s",
		req.TenantID,
		req.RecordingID,
		strings.TrimSpace(req.RecordedAt),
		runtime.RuleSetCode,
		runtime.RuleSetVersion,
		runtime.PromptCode,
		runtime.PromptVersion,
		modelConfigID,
		inputFingerprint,
	)
	// Preserve the established delivery key so deployment does not reanalyse all
	// existing inputs. A manual rerun is distinct, but retries of it are idempotent.
	if strings.TrimSpace(req.TriggerReason) == "manual_rerun" {
		sum := sha256.Sum256([]byte(strings.TrimSpace(req.RerunKey)))
		key += ":manual_rerun:" + hex.EncodeToString(sum[:])
	}
	return key
}

// claimCommunicationRun 先用唯一执行键抢占 processing 记录，再决定是否调用
// LLM。并发重复请求只有一个请求可以成功插入，其他请求直接读取已有结果。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) claimCommunicationRun(ctx context.Context, req CommunicationCheckRequest, runtime *communicationRuntime, modelConfig *complianceModelConfig, inputFingerprint, executionKey string) (*CommunicationCheckResponse, bool, error) {
	var runID string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO compliance_check_runs (
			tenant_id, source_type, source_id, source_version, source_fingerprint,
			subject, scene_code, role_code, employee_id, input_fingerprint,
			prompt_code, prompt_version, rule_version, model_config_id, model_code,
			execution_key, status, started_at
		)
		VALUES ($1, 'recording', $2, $3, $4, 'communication', $5, $6, $7, $4,
		        $8, $9, $10, $11, $12, $13, 'processing', NOW())
		ON CONFLICT (execution_key) DO NOTHING
		RETURNING id::text
	`,
		req.TenantID,
		fmt.Sprintf("%d", req.RecordingID),
		strings.TrimSpace(req.RecordedAt),
		inputFingerprint,
		runtime.RuleSetCode,
		strings.TrimSpace(req.RoleCode),
		req.EmployeeID,
		runtime.PromptCode,
		runtime.PromptVersion,
		runtime.RuleSetVersion,
		modelConfig.ID,
		modelConfig.ModelCode,
		executionKey,
	).Scan(&runID)
	if err == nil {
		return &CommunicationCheckResponse{
			Code:           "PROCESSING",
			Status:         "processing",
			Message:        "communication check processing",
			RequestID:      runID,
			RunID:          runID,
			TenantID:       req.TenantID,
			RecordingID:    req.RecordingID,
			EncounterID:    req.EncounterID,
			EmployeeID:     req.EmployeeID,
			CustomerID:     req.CustomerID,
			RuleSetCode:    runtime.RuleSetCode,
			RuleSetVersion: runtime.RuleSetVersion,
			PromptCode:     runtime.PromptCode,
			PromptVersion:  runtime.PromptVersion,
			ModelConfigID:  modelConfig.ID,
			ModelCode:      modelConfig.ModelCode,
			Findings:       []any{},
		}, true, nil
	}
	if err != pgx.ErrNoRows {
		return nil, false, fmt.Errorf("claim communication run: %w", err)
	}

	stored, err := s.loadStoredCommunicationRun(ctx, executionKey)
	if err != nil {
		return nil, false, err
	}
	return stored.toResponse(req), false, nil
}

func (s *Service) loadStoredCommunicationRun(ctx context.Context, executionKey string) (*storedCommunicationResult, error) {
	var stored storedCommunicationResult
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, status, tenant_id, source_id, source_version,
		       scene_code, rule_version, role_code, COALESCE(employee_id, 0),
		       prompt_code, prompt_version, COALESCE(model_config_id, 0),
		       model_code, finding_count, raw_response, result_json, error_message
		FROM compliance_check_runs
		WHERE execution_key = $1
	`, executionKey).Scan(
		&stored.RunID,
		&stored.Status,
		&stored.TenantID,
		&stored.SourceID,
		&stored.SourceVersion,
		&stored.RuleSetCode,
		&stored.RuleSetVersion,
		&stored.RoleCode,
		&stored.EmployeeID,
		&stored.PromptCode,
		&stored.PromptVersion,
		&stored.ModelConfigID,
		&stored.ModelCode,
		&stored.FindingCount,
		&stored.RawResponse,
		&stored.ResultJSON,
		&stored.ErrorMessage,
	)
	if err != nil {
		return nil, fmt.Errorf("load communication run %s: %w", executionKey, err)
	}
	return &stored, nil
}

func (r *storedCommunicationResult) toResponse(req CommunicationCheckRequest) *CommunicationCheckResponse {
	findings := []any{}
	var result struct {
		Findings []any `json:"findings"`
	}
	if json.Unmarshal(r.ResultJSON, &result) == nil && result.Findings != nil {
		findings = result.Findings
	}
	rawResponse := ""
	var gateway struct {
		Content string `json:"content"`
	}
	if json.Unmarshal(r.RawResponse, &gateway) == nil {
		rawResponse = gateway.Content
	}
	code := "PROCESSING"
	message := "communication check processing"
	switch r.Status {
	case "completed":
		code = "COMPLETED"
		message = "communication check completed"
	case "failed":
		code = "FAILED"
		message = r.ErrorMessage
	}
	return &CommunicationCheckResponse{
		Code:           code,
		Status:         r.Status,
		Message:        message,
		RequestID:      r.RunID,
		RunID:          r.RunID,
		TenantID:       req.TenantID,
		RecordingID:    req.RecordingID,
		EncounterID:    req.EncounterID,
		EmployeeID:     req.EmployeeID,
		CustomerID:     req.CustomerID,
		RuleSetCode:    r.RuleSetCode,
		RuleSetVersion: r.RuleSetVersion,
		PromptCode:     r.PromptCode,
		PromptVersion:  r.PromptVersion,
		ModelConfigID:  r.ModelConfigID,
		ModelCode:      r.ModelCode,
		RawResponse:    rawResponse,
		Findings:       findings,
	}
}

// failCommunicationRun 将模型调用或结果解析失败持久化，避免失败请求在
// 结果表中看起来像“从未执行”。本阶段不自动重试。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) failCommunicationRun(ctx context.Context, requestID string, runtime *communicationRuntime, inputFingerprint, executionKey string, cause error) error {
	rawResponse := jsonObjectOrEmpty(runtime.GatewayResponseJSON)
	_, err := s.pool.Exec(ctx, `
		UPDATE compliance_check_runs
		SET status = 'failed',
		    input_fingerprint = $1,
		    raw_response = $2::jsonb,
		    error_message = $3,
		    completed_at = NOW(),
		    updated_at = NOW()
		WHERE id::text = $4 AND execution_key = $5
	`, inputFingerprint, rawResponse, cause.Error(), requestID, executionKey)
	if err != nil {
		return fmt.Errorf("persist failed communication run: %w", err)
	}
	return nil
}

// completeCommunicationRun 将执行结果写入 run，并把每条发现拆成可查询的
// compliance_findings 记录。两部分在同一事务中提交，避免 run 与发现不一致。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) completeCommunicationRun(ctx context.Context, response *CommunicationCheckResponse, runtime *communicationRuntime, inputFingerprint, executionKey string) error {
	// Validate again at the persistence boundary so callers cannot bypass the
	// parser and write an unverified model result.
	raw, err := json.Marshal(map[string]any{"findings": response.Findings})
	if err != nil {
		return fmt.Errorf("encode findings for validation: %w", err)
	}
	if _, err := parseFindingsForRuntime(string(raw), runtime.Request.CleanedTranscript, runtimeRules(runtime), runtime.OutputContract); err != nil {
		return fmt.Errorf("reject unverified communication result before persistence: %w", err)
	}
	if s.pool == nil {
		return fmt.Errorf("database pool is not configured")
	}
	findingRuleCodes := make([]string, 0, len(response.Findings))
	for _, finding := range response.Findings {
		if item, ok := finding.(map[string]any); ok {
			if code, ok := item["rule_code"].(string); ok && strings.TrimSpace(code) != "" {
				findingRuleCodes = append(findingRuleCodes, code)
			}
		}
	}
	resultJSON, err := json.Marshal(map[string]any{
		"findings": response.Findings,
	})
	if err != nil {
		return fmt.Errorf("encode communication result: %w", err)
	}
	rawResponse := jsonObjectOrEmpty(runtime.GatewayResponseJSON)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin persist communication run: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	_, err = tx.Exec(ctx, `
		UPDATE compliance_check_runs
		SET status = 'completed',
		    input_fingerprint = $1,
		    finding_count = $2,
		    finding_rule_codes = $3::jsonb,
		    raw_response = $4::jsonb,
		    result_json = $5::jsonb,
		    error_message = '',
		    completed_at = NOW(),
		    updated_at = NOW()
		WHERE id::text = $6 AND execution_key = $7
	`, inputFingerprint, len(response.Findings), mustMarshalIndent(findingRuleCodes), rawResponse, string(resultJSON), response.RunID, executionKey)
	if err != nil {
		return fmt.Errorf("persist completed communication run: %w", err)
	}

	for _, finding := range response.Findings {
		item, ok := finding.(map[string]any)
		if !ok {
			return fmt.Errorf("communication finding is not an object")
		}
		ruleCode, _ := item["rule_code"].(string)
		if strings.TrimSpace(ruleCode) == "" {
			return fmt.Errorf("communication finding rule_code is empty")
		}
		evidence := jsonArrayOrEmpty(item["evidence"])
		missingFacts := jsonArrayOrEmpty(item["missing_facts"])
		var findingID string
		err = tx.QueryRow(ctx, `
			INSERT INTO compliance_findings (
				run_id, tenant_id, source_type, source_id, source_version,
				subject, scene_code, role_code, employee_id, rule_code,
				rule_version, fact, summary, reason, evidence, missing_facts,
				needs_review, status
			)
			SELECT id, tenant_id, source_type, source_id, source_version,
			       subject, scene_code, role_code, employee_id, $2,
			       rule_version, $3, $4, $5, $6::jsonb, $7::jsonb,
			       $8, 'pending'
			FROM compliance_check_runs
			WHERE id::text = $1 AND execution_key = $9
			ON CONFLICT (run_id, rule_code) DO UPDATE SET
				fact = EXCLUDED.fact,
				summary = EXCLUDED.summary,
				reason = EXCLUDED.reason,
				evidence = EXCLUDED.evidence,
				missing_facts = EXCLUDED.missing_facts,
				needs_review = EXCLUDED.needs_review,
				updated_at = NOW()
			RETURNING id::text
		`,
			response.RunID,
			strings.TrimSpace(ruleCode),
			stringValue(item["fact"]),
			stringValue(item["summary"]),
			stringValue(item["reason"]),
			evidence,
			missingFacts,
			boolValue(item["needs_review"], true),
			executionKey,
		).Scan(&findingID)
		if err != nil {
			return fmt.Errorf("persist communication finding %s: %w", ruleCode, err)
		}
		if runtime.OutputContract == "risk_candidate_v1" {
			if err := upsertCommunicationCheckItem(ctx, tx, findingID, response.RunID, ruleCode, item); err != nil {
				return fmt.Errorf("create check item for finding %s: %w", ruleCode, err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit communication run: %w", err)
	}
	return nil
}

func upsertCommunicationCheckItem(ctx context.Context, tx pgx.Tx, findingID, runID, ruleCode string, item map[string]any) error {
	themeKey := stringValue(item["theme_key"])
	question := stringValue(item["question"])
	riskHypothesis := stringValue(item["risk_hypothesis"])
	primaryRisk := stringValue(item["primary_risk_type"])
	priority := stringValue(item["priority"])
	if themeKey == "" || question == "" || riskHypothesis == "" || primaryRisk == "" || priority == "" {
		return fmt.Errorf("missing check item fields")
	}
	related := jsonArrayOrEmpty(item["related_risk_types"])
	evidence := jsonArrayOrEmpty(item["evidence"])
	basis := jsonArrayOrEmpty(item["check_basis"])
	actions := jsonArrayOrEmpty(item["suggested_actions"])
	_, err := tx.Exec(ctx, `
		INSERT INTO compliance_check_items (
		 tenant_id, subject, source_type, source_id, source_version, scene_code, role_code, employee_id,
		 theme_key, question, risk_hypothesis, primary_risk_type, related_risk_types, evidence,
		 check_basis, suggested_actions, priority
		)
		SELECT tenant_id, subject, source_type, source_id, source_version, scene_code, role_code, employee_id,
		 $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8::jsonb, $9::jsonb, $10
		FROM compliance_findings WHERE id = $1
		ON CONFLICT (tenant_id, source_type, source_id, source_version, theme_key) DO UPDATE SET
		 question=EXCLUDED.question, risk_hypothesis=EXCLUDED.risk_hypothesis,
		 primary_risk_type=EXCLUDED.primary_risk_type, related_risk_types=EXCLUDED.related_risk_types,
		 evidence=EXCLUDED.evidence, check_basis=EXCLUDED.check_basis,
		 suggested_actions=EXCLUDED.suggested_actions, priority=EXCLUDED.priority, updated_at=NOW()
	`, findingID, themeKey, question, riskHypothesis, primaryRisk, related, evidence, basis, actions, priority)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO compliance_check_item_sources (item_id, finding_id, run_id, rule_code)
		SELECT i.id, $1, $2, $3 FROM compliance_check_items i
		JOIN compliance_findings f ON f.id=$1
		WHERE i.tenant_id=f.tenant_id AND i.source_type=f.source_type AND i.source_id=f.source_id
		  AND i.source_version=f.source_version AND i.theme_key=$4
		ON CONFLICT (item_id, finding_id) DO UPDATE SET rule_code=EXCLUDED.rule_code
	`, findingID, runID, ruleCode, themeKey)
	return err
}

func jsonObjectOrEmpty(raw string) string {
	var value map[string]any
	if json.Unmarshal([]byte(raw), &value) == nil && value != nil {
		return raw
	}
	return "{}"
}

func jsonArrayOrEmpty(value any) string {
	if value == nil {
		return "[]"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	var array []any
	if json.Unmarshal(raw, &array) != nil {
		return "[]"
	}
	return string(raw)
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func boolValue(value any, fallback bool) bool {
	if value == nil {
		return fallback
	}
	parsed, ok := value.(bool)
	if !ok {
		return fallback
	}
	return parsed
}

type communicationRuntime struct {
	Request             CommunicationCheckRequest
	RuleSetCode         string
	RuleSetName         string
	RuleSetVersion      string
	PromptCode          string
	OutputContract      string
	PromptVersion       string
	PromptText          string
	RulesJSON           string
	SystemPrompt        string
	UserPrompt          string
	ModelConfigID       int64
	ModelTenantID       int64
	ModelFunctionType   string
	Provider            string
	ModelCode           string
	ModelParamsJSON     string
	GatewayRequestJSON  string
	GatewayResponseJSON string
	FormattedResult     string
	GatewayError        string
}

type communicationPromptRuntime struct {
	Code           string
	SystemPrompt   string
	UserTemplate   string
	Version        string
	PromptText     string
	OutputSchema   []byte
	OutputContract string
}

type communicationRuleDocument struct {
	RuleCode              string   `json:"rule_code"`
	LegacyRuleNumber      string   `json:"legacy_rule_number"`
	Name                  string   `json:"name"`
	Category              string   `json:"category"`
	Subject               string   `json:"subject"`
	Status                string   `json:"status"`
	ApplicableRuleSets    []string `json:"applicable_rule_sets"`
	LegalBasis            []string `json:"legal_basis"`
	JudgmentStandard      string   `json:"judgment_standard"`
	TriggerConditions     []string `json:"trigger_conditions"`
	ExclusionConditions   []string `json:"exclusion_conditions"`
	ViolationExamples     []string `json:"violation_examples"`
	CompliantExamples     []string `json:"compliant_examples"`
	FactDefinition        string   `json:"fact_definition"`
	RequiredEvidence      []string `json:"required_evidence"`
	RequiredExternalFacts []string `json:"required_external_facts"`
	IndeterminateBoundary string   `json:"indeterminate_boundary"`
	OutputType            string   `json:"output_type"`
	ReviewPriority        string   `json:"review_priority"`
	ValidationStatus      string   `json:"validation_status"`
	SourceBasis           []string `json:"source_basis"`
}

type communicationRuleSetDocument struct {
	Code              string   `json:"code"`
	Name              string   `json:"name"`
	Subject           string   `json:"subject"`
	Status            string   `json:"status"`
	RoleCodes         []string `json:"role_codes"`
	SourceTypes       []string `json:"source_types"`
	IncludedRuleCodes []string `json:"included_rule_codes"`
	Notes             string   `json:"notes"`
}

// buildCommunicationRuntime 从数据库读取提示词和规则集，
// 并生成本次请求的实际提示词。
//
// 数据库中的规则目录是合规卫士当前确认的唯一运行时规则基准；本函数只
// 选取当前规则集中的 published 规则，不把内容规则或 draft 规则混入沟通检查。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) buildCommunicationRuntime(ctx context.Context, req CommunicationCheckRequest) (*communicationRuntime, error) {
	ruleSetCode := defaultCommunicationRuleSetCode(req.BusinessScope, req.RoleCode)
	prompt, err := s.loadCommunicationPrompt(ctx, req.TenantID, ruleSetCode)
	if err != nil {
		return nil, err
	}
	ruleSet, selectedRules, ruleSetVersion, err := s.loadCommunicationRules(ctx, req.TenantID, ruleSetCode)
	if err != nil {
		return nil, err
	}
	selectedRuleBytes, err := json.MarshalIndent(selectedRules, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode selected communication rules: %w", err)
	}

	vars := map[string]string{
		"tenant_id":        fmt.Sprintf("%d", req.TenantID),
		"source_type":      "recording",
		"source_id":        fmt.Sprintf("%d", req.RecordingID),
		"source_version":   req.RecordedAt,
		"scene_code":       ruleSetCode,
		"role_code":        strings.TrimSpace(req.RoleCode),
		"employee_name":    communicationEmployeeName(req),
		"department":       "",
		"recorded_at":      strings.TrimSpace(req.RecordedAt),
		"rule_set_code":    ruleSetCode,
		"rule_set_version": ruleSetVersion,
		"rules_json":       string(selectedRuleBytes),
		"transcript":       req.CleanedTranscript,
		"input_text":       req.CleanedTranscript,
	}
	systemPrompt := renderCommunicationTemplate(prompt.SystemPrompt, vars)
	userPrompt := renderCommunicationTemplate(prompt.UserTemplate, vars)
	userPrompt = ensureCommunicationPromptContext(userPrompt, string(selectedRuleBytes), req.CleanedTranscript)

	return &communicationRuntime{
		Request:        req,
		OutputContract: prompt.OutputContract,
		RuleSetCode:    ruleSetCode,
		RuleSetName:    ruleSet.Name,
		RuleSetVersion: ruleSetVersion,
		PromptCode:     prompt.Code,
		PromptVersion:  prompt.Version,
		PromptText:     prompt.PromptText,
		RulesJSON:      string(selectedRuleBytes),
		SystemPrompt:   systemPrompt,
		UserPrompt:     userPrompt,
	}, nil
}

// loadCommunicationRules 从数据库读取指定租户可用的规则集和成员规则。
//
// 规则集版本、规则状态和成员顺序全部由数据库决定；API 不再读取本地
// JSON 文件，也不依赖文件路径或文件名。当前只读取平台全局目录，保留
// tenant_id 条件为后续本院规则扩展预留。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (s *Service) loadCommunicationRules(ctx context.Context, tenantID int64, ruleSetCode string) (*communicationRuleSetDocument, []communicationRuleDocument, string, error) {
	if s.pool == nil {
		return nil, nil, "", fmt.Errorf("database pool is not configured")
	}

	roleCode := communicationRuleRole(ruleSetCode)
	if roleCode == "" {
		return nil, nil, "", fmt.Errorf("unsupported communication rule set %s", ruleSetCode)
	}
	var ruleSetVersion string
	err := s.pool.QueryRow(ctx, `
		SELECT version
		FROM compliance_rules
		WHERE subject = 'communication'
		  AND status = 'published'
		  AND is_enabled = true
		  AND scene_codes @> jsonb_build_array($1::text)
		  AND role_codes @> jsonb_build_array($2::text)
		ORDER BY version DESC, updated_at DESC
		LIMIT 1
	`, ruleSetCode, roleCode).Scan(&ruleSetVersion)
	if err != nil {
		return nil, nil, "", fmt.Errorf("load communication rule version %s: %w", ruleSetCode, err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT rule_code, rule_name, subject, status, judgment_standard, definition
		FROM compliance_rules
		WHERE subject = 'communication'
		  AND status = 'published'
		  AND is_enabled = true
		  AND version = $1
		  AND scene_codes @> jsonb_build_array($2::text)
		  AND role_codes @> jsonb_build_array($3::text)
		ORDER BY id
	`, ruleSetVersion, ruleSetCode, roleCode)
	if err != nil {
		return nil, nil, "", fmt.Errorf("load communication rules for %s: %w", ruleSetCode, err)
	}
	defer rows.Close()

	selectedRules := make([]communicationRuleDocument, 0)
	for rows.Next() {
		var rule communicationRuleDocument
		var definition []byte
		if err := rows.Scan(
			&rule.RuleCode,
			&rule.Name,
			&rule.Subject,
			&rule.Status,
			&rule.JudgmentStandard,
			&definition,
		); err != nil {
			return nil, nil, "", fmt.Errorf("scan communication rule for %s: %w", ruleSetCode, err)
		}
		if err := json.Unmarshal(definition, &rule); err != nil {
			return nil, nil, "", fmt.Errorf("decode definition for rule %s: %w", rule.RuleCode, err)
		}
		rule.RuleCode = strings.TrimSpace(rule.RuleCode)
		if rule.RuleCode == "" {
			return nil, nil, "", fmt.Errorf("communication rule %s has empty rule_code", ruleSetCode)
		}
		rule.Subject = "communication"
		rule.Status = "published"
		selectedRules = append(selectedRules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, "", fmt.Errorf("iterate communication rules for %s: %w", ruleSetCode, err)
	}
	if len(selectedRules) == 0 {
		return nil, nil, "", fmt.Errorf("communication rule set %s has no published rules", ruleSetCode)
	}
	return &communicationRuleSetDocument{
		Code:              ruleSetCode,
		Name:              communicationRuleSetName(ruleSetCode),
		Subject:           "communication",
		Status:            "published",
		RoleCodes:         []string{roleCode},
		SourceTypes:       []string{"recording"},
		IncludedRuleCodes: ruleCodes(selectedRules),
	}, selectedRules, ruleSetVersion, nil
}

func communicationRuleRole(ruleSetCode string) string {
	switch ruleSetCode {
	case "communication.doctor.outpatient":
		return "doctor"
	case "communication.consultant.consultation":
		return "consultant"
	default:
		return ""
	}
}

func communicationRuleSetName(ruleSetCode string) string {
	switch ruleSetCode {
	case "communication.doctor.outpatient":
		return "医生门诊沟通合规"
	case "communication.consultant.consultation":
		return "咨询师沟通合规"
	default:
		return ruleSetCode
	}
}

func ruleCodes(rules []communicationRuleDocument) []string {
	codes := make([]string, 0, len(rules))
	for _, rule := range rules {
		codes = append(codes, rule.RuleCode)
	}
	return codes
}

func renderCommunicationTemplate(template string, vars map[string]string) string {
	rendered := template
	for key, value := range vars {
		rendered = strings.ReplaceAll(rendered, "{{"+key+"}}", value)
	}
	return rendered
}

// startCommunicationProcessLog 在 Handler 读到请求体后立即创建过程文件。
//
// 文件名不依赖请求是否能成功解析，因此 malformed JSON、缺少字段、读取规则
// 失败等请求也能留下独立记录。正式内容由 finishCommunicationProcessLog 覆盖。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
func (s *Service) startCommunicationProcessLog(requestBody []byte) (string, error) {
	now := time.Now().UTC()
	recordingID := int64(0)
	var request CommunicationCheckRequest
	if err := json.Unmarshal(requestBody, &request); err == nil {
		recordingID = request.RecordingID
	}
	recordingPart := "request"
	if recordingID > 0 {
		recordingPart = fmt.Sprintf("recording-%d", recordingID)
	}
	filename := fmt.Sprintf(
		"communication-check-process-%s-%s.txt",
		now.Format("20060102T150405.000000000Z"),
		recordingPart,
	)
	path := filepath.Join(complianceProcessLogDir, filename)
	content := fmt.Sprintf("沟通合规请求过程记录\n时间：%s\n处理阶段：已收到请求\n\n===== 请求原文 =====\n%s\n", now.Format(time.RFC3339Nano), string(requestBody))

	s.processLogMu.Lock()
	defer s.processLogMu.Unlock()
	if err := os.MkdirAll(complianceProcessLogDir, 0o755); err != nil {
		return "", fmt.Errorf("create compliance process log directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write compliance process log: %w", err)
	}
	return path, nil
}

// finishCommunicationProcessLog 把请求处理的最终阶段写回已创建的独立过程文件。
//
// runtime 为空时，文件仍然保留请求原文、响应和错误，保证失败请求不再“没有
// 过程记录”。不追加现有 communication-check.log，避免接口日志和提示词调试
// 内容混在一起。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
func (s *Service) finishCommunicationProcessLog(path string, requestBody []byte, runtime *communicationRuntime, status int, responseBody, processError string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("communication process log path is empty")
	}
	now := time.Now().UTC()
	content := fmt.Sprintf(
		"沟通合规请求过程记录\n"+
			"时间：%s\n"+
			"处理阶段：已完成\n"+
			"响应状态：%d\n"+
			"处理错误：%s\n\n"+
			"===== 请求原文 =====\n%s\n\n"+
			"===== 响应内容 =====\n%s\n",
		now.Format(time.RFC3339Nano),
		status,
		processError,
		string(requestBody),
		responseBody,
	)
	if runtime != nil {
		content += fmt.Sprintf(
			"\n===== 规则集 =====\n%s\n"+
				"规则集名称：%s\n"+
				"规则集版本：%s\n\n"+
				"===== 数据库提示词 =====\n%s\n\n"+
				"===== 本次选用的规则 =====\n%s\n\n"+
				"===== 最终 System Prompt =====\n%s\n\n"+
				"===== 最终 User Prompt =====\n%s\n"+
				"\n===== 模型配置 =====\n"+
				"配置 ID：%d\n"+
				"配置租户 ID：%d\n"+
				"功能类型：%s\n"+
				"Provider：%s\n"+
				"模型代码：%s\n"+
				"模型参数：%s\n"+
				"\n===== LLM 网关请求原始 JSON =====\n%s\n"+
				"\n===== LLM 网关响应原始 JSON =====\n%s\n"+
				"\n===== 格式化审核结果 =====\n%s\n"+
				"\n===== LLM 调用错误 =====\n%s\n",
			runtime.RuleSetCode,
			runtime.RuleSetName,
			runtime.RuleSetVersion,
			runtime.PromptText,
			runtime.RulesJSON,
			runtime.SystemPrompt,
			runtime.UserPrompt,
			runtime.ModelConfigID,
			runtime.ModelTenantID,
			runtime.ModelFunctionType,
			runtime.Provider,
			runtime.ModelCode,
			runtime.ModelParamsJSON,
			runtime.GatewayRequestJSON,
			runtime.GatewayResponseJSON,
			runtime.FormattedResult,
			runtime.GatewayError,
		)
	}

	s.processLogMu.Lock()
	defer s.processLogMu.Unlock()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("finish compliance process log: %w", err)
	}
	return nil
}

func mustMarshalIndent(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func defaultCommunicationRuleSetCode(businessScope, roleCode string) string {
	switch strings.ToLower(strings.TrimSpace(businessScope)) {
	case "consultant":
		return "communication.consultant.consultation"
	default:
		switch strings.ToLower(strings.TrimSpace(roleCode)) {
		case "consultant":
			return "communication.consultant.consultation"
		default:
			return "communication.doctor.outpatient"
		}
	}
}

// validateCommunicationRole 只拒绝当前明确不支持的 sales 角色。
// 其他既有角色保持原有路由行为，避免本次兼容改造扩大业务范围。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-13
func validateCommunicationRole(businessScope, roleCode string) error {
	if strings.EqualFold(strings.TrimSpace(businessScope), "sales") ||
		strings.EqualFold(strings.TrimSpace(roleCode), "sales") {
		return &UnsupportedRoleError{Message: "sales role is not supported by communication compliance"}
	}
	return nil
}

func communicationEmployeeName(req CommunicationCheckRequest) string {
	if name := strings.TrimSpace(req.EmployeeName); name != "" {
		return name
	}
	return strings.TrimSpace(req.DoctorName)
}

func (s *Service) AnalyzeText(ctx context.Context, req AnalyzeTextRequest) (*AnalyzeTextResponse, error) {
	if req.TenantID <= 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(req.SceneCode) == "" {
		return nil, fmt.Errorf("scene_code is required")
	}
	if strings.TrimSpace(req.SourceType) == "" || strings.TrimSpace(req.SourceID) == "" || strings.TrimSpace(req.SourceVersion) == "" {
		return nil, fmt.Errorf("source_type, source_id and source_version are required")
	}
	if strings.TrimSpace(req.Text) == "" {
		return nil, fmt.Errorf("text is required")
	}
	if s.llm == nil {
		return nil, fmt.Errorf("llm client is required")
	}

	runtime, err := s.buildCommunicationRuntime(ctx, CommunicationCheckRequest{
		TenantID:          req.TenantID,
		RecordingID:       parseInt64(req.SourceID),
		BusinessScope:     req.SceneCode,
		RoleCode:          req.SceneCode,
		RecordedAt:        req.SourceVersion,
		CleanedTranscript: req.Text,
	})
	if err != nil {
		return nil, err
	}
	modelConfig, err := s.loadModelConfig(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}

	llmRequest := buildCommunicationLLMRequest(req.TenantID, req.SourceID, runtime.RuleSetCode, runtime.RuleSetCode, runtime.RuleSetVersion, runtime.RuleSetName, modelConfig, runtime)
	resp, err := s.llm.TextInference(ctx, llmRequest)
	if err != nil {
		return &AnalyzeTextResponse{Status: "failed", PromptCode: runtime.PromptCode, PromptVersion: runtime.PromptVersion, RuleSetCode: runtime.RuleSetCode, RuleSetVersion: runtime.RuleSetVersion, Error: err.Error()}, err
	}

	findings, err := parseFindingsForRuntime(resp.Content, req.Text, runtimeRules(runtime), runtime.OutputContract)
	if err != nil {
		return &AnalyzeTextResponse{Status: "failed", PromptCode: runtime.PromptCode, PromptVersion: runtime.PromptVersion, RuleSetCode: runtime.RuleSetCode, RuleSetVersion: runtime.RuleSetVersion, RawResponse: resp.Content, Error: err.Error()}, err
	}

	return &AnalyzeTextResponse{
		RunID:          req.SourceID,
		Status:         "completed",
		PromptCode:     runtime.PromptCode,
		PromptVersion:  runtime.PromptVersion,
		ModelCode:      resp.ModelCode,
		RuleSetCode:    runtime.RuleSetCode,
		RuleSetVersion: runtime.RuleSetVersion,
		RawResponse:    resp.Content,
		Findings:       findings,
		FinishedAt:     time.Now().UTC(),
	}, nil
}

// loadModelConfig 按功能类型选择当前有效的合规检查模型配置。
//
// 当前租户配置优先，全局配置（tenant_id=0）兜底；同一范围内优先默认配置，
// 再按更新时间和 ID 取最新记录。代码不绑定具体配置 ID 或模型名称。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-12
func (s *Service) loadModelConfig(ctx context.Context, tenantID int64) (*complianceModelConfig, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}

	var config complianceModelConfig
	var rawParams []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id,
		       COALESCE(tenant_id, 0),
		       COALESCE(function_type, ''),
		       COALESCE(provider, ''),
		       COALESCE(model_code, ''),
		       COALESCE(model_params, extra_params, '{}'::json)
		FROM llm_model_configs
		WHERE deleted_at IS NULL
		  AND COALESCE(is_active, true) = true
		  AND function_type = 'compliance_check'
		  AND tenant_id IN ($1, 0)
		ORDER BY CASE WHEN tenant_id = $1 THEN 0 ELSE 1 END,
		         CASE WHEN COALESCE(is_default, false) THEN 0 ELSE 1 END,
		         updated_at DESC,
		         id DESC
		LIMIT 1
	`, tenantID).Scan(
		&config.ID,
		&config.TenantID,
		&config.FunctionType,
		&config.Provider,
		&config.ModelCode,
		&rawParams,
	)
	if err != nil {
		return nil, fmt.Errorf("load compliance model config: %w", err)
	}
	if strings.TrimSpace(config.Provider) == "" || strings.TrimSpace(config.ModelCode) == "" {
		return nil, fmt.Errorf("compliance model config is incomplete")
	}
	config.ModelParams = map[string]any{}
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &config.ModelParams); err != nil {
			return nil, fmt.Errorf("decode compliance model params: %w", err)
		}
	}
	return &config, nil
}

// buildCommunicationLLMRequest 将数据库配置和本次沟通运行时内容转换为网关契约。
//
// provider、model_code 和模型参数全部来自数据库配置，不在合规业务代码中指定
// 具体模型名称。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-12
func buildCommunicationLLMRequest(tenantID int64, sourceID, sceneCode, ruleSetCode, ruleSetVersion, ruleSetName string, config *complianceModelConfig, runtime *communicationRuntime) llmgateway.TextInferenceRequest {
	params := &llmgateway.Params{
		Temperature:    0,
		MaxTokens:      4000,
		TimeoutSeconds: 120,
		ResponseFormat: "json",
	}
	applyCommunicationModelParams(params, config.ModelParams)
	return llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  "compliance.communication",
		TraceID:       fmt.Sprintf("compliance-%d-%s", tenantID, sourceID),
		FunctionType:  config.FunctionType,
		Provider:      config.Provider,
		ModelCode:     config.ModelCode,
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "compliance",
			BusinessObjectType: "recording",
			BusinessObjectID:   parseInt64(sourceID),
			BillingSubject:     "communication_check",
			BillingScene:       ruleSetCode,
			BillingRuleVersion: ruleSetVersion,
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: runtime.SystemPrompt},
			{Role: "user", Content: runtime.UserPrompt},
		},
		Params: params,
	}
}

func applyCommunicationModelParams(params *llmgateway.Params, raw map[string]any) {
	if value, ok := raw["temperature"].(float64); ok {
		params.Temperature = value
	}
	if value, ok := raw["max_tokens"].(float64); ok && value > 0 {
		params.MaxTokens = int(value)
	}
	if value, ok := raw["timeout_seconds"].(float64); ok && value > 0 {
		params.TimeoutSeconds = int(value)
	}
	if value, ok := raw["response_format"].(string); ok && strings.TrimSpace(value) != "" {
		params.ResponseFormat = value
	}
}

func parseFindings(raw, transcript string, rules []string) ([]any, error) {
	return decodeCommunicationFindings(raw, transcript, rules)
}

func parseFindingsForRuntime(raw, transcript string, rules []string, contract string) ([]any, error) {
	if contract == "risk_candidate_v1" {
		return parseFindings(raw, transcript, rules)
	}
	return parseLegacyFindings(raw, transcript, rules)
}

func parseLegacyFindings(raw, transcript string, rules []string) ([]any, error) {
	raw = stripJSONCodeFence(raw)
	var payload struct {
		Findings []any `json:"findings"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return nil, fmt.Errorf("parse legacy compliance model JSON: %w", err)
	}
	if payload.Findings == nil {
		return nil, fmt.Errorf("legacy compliance model JSON does not contain findings")
	}
	allowed := map[string]bool{}
	for _, rule := range rules {
		allowed[rule] = true
	}
	for _, value := range payload.Findings {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("legacy finding must be object")
		}
		code, ok := item["rule_code"].(string)
		if !ok || !allowed[code] {
			return nil, fmt.Errorf("legacy finding rule is not in enabled rules")
		}
		evidence, ok := item["evidence"].([]any)
		if !ok || len(evidence) == 0 {
			return nil, fmt.Errorf("legacy finding evidence required")
		}
		for _, value := range evidence {
			e, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("legacy evidence must be object")
			}
			quote, ok := e["quote"].(string)
			if !ok || strings.TrimSpace(quote) == "" || !strings.Contains(transcript, quote) {
				return nil, fmt.Errorf("legacy evidence is not verbatim")
			}
		}
	}
	return payload.Findings, nil
}

func runtimeRules(runtime *communicationRuntime) []string {
	var documents []communicationRuleDocument
	if json.Unmarshal([]byte(runtime.RulesJSON), &documents) != nil {
		return nil
	}
	codes := make([]string, 0, len(documents))
	for _, rule := range documents {
		codes = append(codes, rule.RuleCode)
	}
	return codes
}

func formatJSONText(raw string) string {
	raw = stripJSONCodeFence(raw)
	var value any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &value); err != nil {
		return raw
	}
	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return raw
	}
	return string(formatted)
}

func stripJSONCodeFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "```") && strings.HasSuffix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 3 {
			lines = lines[1 : len(lines)-1]
			return strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	return trimmed
}

func ensureCommunicationPromptContext(prompt, rulesJSON, transcript string) string {
	trimmed := strings.TrimSpace(prompt)
	if !strings.Contains(trimmed, strings.TrimSpace(transcript)) && strings.TrimSpace(transcript) != "" {
		trimmed += "\n\n【沟通资料】\n" + transcript
	}
	if strings.TrimSpace(rulesJSON) != "" && !strings.Contains(trimmed, strings.TrimSpace(rulesJSON)) {
		trimmed += "\n\n【本次适用规则】\n" + rulesJSON
	}
	return trimmed
}

func parseInt64(raw string) int64 {
	var value int64
	_, _ = fmt.Sscanf(strings.TrimSpace(raw), "%d", &value)
	return value
}

// loadCommunicationPrompt 从全局提示词和当前租户覆盖中读取正式运行提示词。
//
// data 目录中的备份文本不参与运行时读取；提示词缺失或未启用时直接报错，
// 不回退到代码或本地文件，保证数据库才是唯一运行时来源。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-13
func communicationPromptCodes(ruleSetCode string) []string {
	if ruleSetCode == "communication.doctor.outpatient" {
		return []string{complianceDoctorPromptCode, compliancePromptCode}
	}
	return []string{compliancePromptCode}
}

func (s *Service) loadCommunicationPrompt(ctx context.Context, tenantID int64, ruleSetCode string) (*communicationPromptRuntime, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is not configured")
	}

	var cfg communicationPromptRuntime
	err := s.pool.QueryRow(ctx, `
		SELECT code, COALESCE(system_prompt, ''), COALESCE(user_prompt_template, ''),
		       COALESCE(version, 'v1'), output_schema
		FROM recording_analysis_prompts
		WHERE code = ANY($1::text[]) AND is_active = true
		ORDER BY array_position($1::text[], code::text), id DESC
		LIMIT 1
	`, communicationPromptCodes(ruleSetCode)).Scan(&cfg.Code, &cfg.SystemPrompt, &cfg.UserTemplate, &cfg.Version, &cfg.OutputSchema)
	if err != nil {
		return nil, fmt.Errorf("load global compliance prompt: %w", err)
	}

	var customSystemPrompt, customUserPromptTemplate, additionalInstructions *string
	var customOutputSchema []byte
	err = s.pool.QueryRow(ctx, `
		SELECT custom_system_prompt, custom_user_prompt_template, custom_output_schema, additional_instructions
		FROM recording_analysis_tenant_configs
		WHERE tenant_id = $1 AND prompt_code = $2 AND is_enabled = true
		ORDER BY priority DESC, id DESC
		LIMIT 1
	`, tenantID, cfg.Code).Scan(
		&customSystemPrompt, &customUserPromptTemplate, &customOutputSchema, &additionalInstructions,
	)
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("load tenant compliance prompt override: %w", err)
	}
	if err == nil {
		if customSystemPrompt != nil && strings.TrimSpace(*customSystemPrompt) != "" {
			cfg.SystemPrompt = strings.TrimSpace(*customSystemPrompt)
		}
		if customUserPromptTemplate != nil && strings.TrimSpace(*customUserPromptTemplate) != "" {
			cfg.UserTemplate = strings.TrimSpace(*customUserPromptTemplate)
		}
		if additionalInstructions != nil && strings.TrimSpace(*additionalInstructions) != "" {
			cfg.SystemPrompt = strings.TrimSpace(cfg.SystemPrompt) + "\n\n" + strings.TrimSpace(*additionalInstructions)
		}
		cfg.OutputSchema = selectCommunicationOutputSchema(cfg.OutputSchema, customOutputSchema)
	}
	if strings.TrimSpace(cfg.SystemPrompt) == "" || strings.TrimSpace(cfg.UserTemplate) == "" {
		return nil, fmt.Errorf("compliance prompt %q has empty system or user prompt", cfg.Code)
	}
	contract, err := detectCommunicationOutputContract(cfg.OutputSchema)
	if err != nil {
		return nil, err
	}
	cfg.OutputContract = contract
	cfg.PromptText = cfg.SystemPrompt + "\n\n" + cfg.UserTemplate
	return &cfg, nil
}

func selectCommunicationOutputSchema(globalSchema, customSchema []byte) []byte {
	if strings.TrimSpace(string(customSchema)) != "" {
		return customSchema
	}
	return globalSchema
}

func detectCommunicationOutputContract(schemaJSON []byte) (string, error) {
	var schema struct {
		Type       string `json:"type"`
		Properties struct {
			Findings struct {
				Type  string `json:"type"`
				Items struct {
					Required []string `json:"required"`
				} `json:"items"`
			} `json:"findings"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return "", fmt.Errorf("invalid configured output schema: %w", err)
	}
	if schema.Type != "object" || schema.Properties.Findings.Type != "array" {
		return "", fmt.Errorf("configured output schema missing findings array")
	}
	for _, field := range schema.Properties.Findings.Items.Required {
		if field == "source_rule_codes" {
			return "risk_candidate_v1", nil
		}
	}
	return "legacy", nil
}
