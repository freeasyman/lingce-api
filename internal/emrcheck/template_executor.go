package emrcheck

import (
	"fmt"
	"strings"
)

// TemplateField 是模板结构执行器需要的最小栏位描述。
//
// 模板结构规则只检查模板 schema，不读取具体病历内容。把执行器需要的
// 字段单独抽出，避免模板查询、规则调度和规则判断混在同一个函数里。
//
// 署名：Codex
// 时间：2026-09-14
type TemplateField struct {
	Code      string
	Name      string
	Visible   bool
	Category  string
	InputType string
}

// IsTemplateStructureRule 判断规则是否属于模板结构规则。
//
// 这类规则应在模板保存/发布阶段执行，不能进入每份病历的质检结果，
// 否则同一个模板配置问题会被重复显示到每一份病历上。
//
// 署名：Codex
// 时间：2026-09-14
func IsTemplateStructureRule(code string) bool {
	code = strings.TrimSpace(code)
	return strings.HasPrefix(code, "common.visible_fields.") ||
		strings.HasPrefix(code, "common.orders_split.")
}

// EvaluateTemplateStructureRule 执行当前批次支持的模板结构规则。
//
// 当前数据模型提供模板栏位和栏位分类，没有独立的处方/处置对象 schema，
// 因此 common.orders_split 先验证处方栏位和处置栏位是否作为两个独立、
// 可见栏位存在；不虚构当前数据库无法证明的对象级结构。
//
// 署名：Codex
// 时间：2026-09-14
func EvaluateTemplateStructureRule(code string, fields []TemplateField) (bool, map[string]any, string, string) {
	code = strings.TrimSpace(code)
	index := make(map[string]TemplateField, len(fields))
	for _, field := range fields {
		field.Code = strings.TrimSpace(field.Code)
		if field.Code != "" {
			index[field.Code] = field
		}
	}

	switch {
	case strings.HasPrefix(code, "common.visible_fields."):
		fieldCode := strings.TrimPrefix(code, "common.visible_fields.")
		field, ok := index[fieldCode]
		evidence := map[string]any{
			"rule_code":  code,
			"field_code": fieldCode,
			"field_name": field.Name,
			"exists":     ok,
			"is_visible": ok && field.Visible,
		}
		if !ok {
			return false, evidence, "模板缺少公共栏位。", "请把该公共栏位加入模板并重新发布。"
		}
		if !field.Visible {
			return false, evidence, "公共栏位存在但不可见。", "请将该公共栏位设置为可见后重新发布。"
		}
		return true, evidence, "公共栏位存在且可见。", ""

	case strings.HasPrefix(code, "common.orders_split."):
		prescription, prescriptionOK := index["prescription"]
		disposition, dispositionOK := index["disposition"]
		evidence := map[string]any{
			"rule_code":             code,
			"prescription_exists":   prescriptionOK,
			"prescription_visible":  prescriptionOK && prescription.Visible,
			"prescription_category": prescription.Category,
			"disposition_exists":    dispositionOK,
			"disposition_visible":   dispositionOK && disposition.Visible,
			"disposition_category":  disposition.Category,
			"fields_are_distinct":   prescriptionOK && dispositionOK && prescription.Code != disposition.Code,
			"validated_scope":       "模板栏位层级",
		}
		if !prescriptionOK || !dispositionOK {
			return false, evidence, "模板没有把处方和处置建模为两个独立栏位。", "请分别保留处方栏位和处置栏位后重新发布。"
		}
		if !prescription.Visible || !disposition.Visible {
			return false, evidence, "处方或处置栏位不可见。", "请将处方和处置栏位设置为可见后重新发布。"
		}
		return true, evidence, "处方和处置已作为两个独立可见栏位配置。", ""
	default:
		return false, map[string]any{"rule_code": code},
			fmt.Sprintf("未识别的模板结构规则：%s。", code), "请检查模板质量规则配置。"
	}
}
