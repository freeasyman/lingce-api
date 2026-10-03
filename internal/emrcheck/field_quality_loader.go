package emrcheck

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FieldQualityRule 栏位中心化质量规则
type FieldQualityRule struct {
	Code           string           `json:"code"`
	FactCode       string           `json:"fact_code"`
	Name           string           `json:"name"`
	Enabled        bool             `json:"enabled"`
	Execution      string           `json:"execution"`       // "program" 或 "llm"
	Executor       string           `json:"executor"`        // 执行器名称
	Parameters     map[string]any   `json:"parameters"`      // 规则参数
	RelatedFields  []string         `json:"related_fields"`  // 关联栏位(多栏位规则)
	LLMInstruction string           `json:"llm_instruction"` // LLM执行指令
	SkipIf         []RuleDependency `json:"skip_if"`         // 程序规则满足条件时跳过本规则
}

// RuleDependency describes a program result that makes an LLM rule redundant
// for the current document.
type RuleDependency struct {
	FieldCode  string `json:"field"`
	RuleCode   string `json:"rule"`
	Conclusion string `json:"conclusion"`
}

// FieldWithQualityRules 携带质量规则的栏位
type FieldWithQualityRules struct {
	FieldID      string
	Code         string
	Name         string
	Description  string
	IsRequired   bool
	QualityRules []FieldQualityRule
}

// FieldQualityLoader 栏位质量规则加载器
type FieldQualityLoader struct {
	pool *pgxpool.Pool
}

func NewFieldQualityLoader(pool *pgxpool.Pool) *FieldQualityLoader {
	return &FieldQualityLoader{pool: pool}
}

// LoadFieldQualityRules 加载指定模板的所有栏位质量规则
func (l *FieldQualityLoader) LoadFieldQualityRules(ctx context.Context, tenantID int64, templateVersionID string) ([]FieldWithQualityRules, error) {
	// 查询模板包含的所有栏位及其质量规则
	rows, err := l.pool.Query(ctx, `
		SELECT
			f.id,
			f.code,
			f.name,
			COALESCE(f.description, ''),
			tf.is_required,
			COALESCE(f.quality_rules, '[]'::jsonb)
		FROM emr_template_fields tf
		JOIN emr_fields f ON f.id = tf.field_id
		WHERE tf.template_id = $1
		  AND tf.is_visible = true
		ORDER BY tf.display_order, f.code
	`, templateVersionID)
	if err != nil {
		return nil, fmt.Errorf("query template fields with quality rules: %w", err)
	}
	defer rows.Close()

	fields := make([]FieldWithQualityRules, 0)
	for rows.Next() {
		var field FieldWithQualityRules
		var rulesJSON []byte

		if err := rows.Scan(&field.FieldID, &field.Code, &field.Name, &field.Description, &field.IsRequired, &rulesJSON); err != nil {
			return nil, fmt.Errorf("scan field quality rules: %w", err)
		}

		// 解析质量规则JSON数组
		if err := json.Unmarshal(rulesJSON, &field.QualityRules); err != nil {
			return nil, fmt.Errorf("unmarshal quality rules for field %s: %w", field.Code, err)
		}

		fields = append(fields, field)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return fields, nil
}

// FlattenQualityRules 将栏位质量规则展平为可执行的规则列表
func (l *FieldQualityLoader) FlattenQualityRules(fields []FieldWithQualityRules) []ExecutableRule {
	rules := make([]ExecutableRule, 0)

	for _, field := range fields {
		for _, rule := range field.QualityRules {
			// 只处理已启用的规则
			if !rule.Enabled {
				continue
			}

			execRule := ExecutableRule{
				FieldCode:      field.Code,
				FieldName:      field.Name,
				FieldRequired:  field.IsRequired,
				RuleCode:       rule.Code,
				FactCode:       rule.FactCode,
				RuleName:       rule.Name,
				Execution:      rule.Execution,
				Executor:       rule.Executor,
				Parameters:     rule.Parameters,
				RelatedFields:  rule.RelatedFields,
				LLMInstruction: rule.LLMInstruction,
				SkipIf:         rule.SkipIf,
			}

			rules = append(rules, execRule)
		}
	}

	return rules
}

// ExecutableRule 可执行的质量规则
type ExecutableRule struct {
	FieldCode      string           // 栏位代码
	FieldName      string           // 栏位名称
	FieldRequired  bool             // 模板中该栏位是否必填
	RuleCode       string           // 规则代码
	FactCode       string           // 同一栏位内可归并的事实代码
	RuleName       string           // 规则名称
	Execution      string           // 执行方式: "program" 或 "llm"
	Executor       string           // 执行器名称
	Parameters     map[string]any   // 规则参数
	RelatedFields  []string         // 关联栏位
	LLMInstruction string           // LLM执行指令
	SkipIf         []RuleDependency // 程序规则满足条件时跳过本规则
}
