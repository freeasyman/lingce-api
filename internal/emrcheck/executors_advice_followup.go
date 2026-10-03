package emrcheck

import (
	"regexp"
	"strings"
	"time"
)

// ========== 医嘱 medical_advice 执行器 ==========

// medicalAdviceExplicitOnly 医嘱仅记录明确交代内容
func medicalAdviceExplicitOnly(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "medical_advice")
	result.Evidence["field"] = "medical_advice"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "医嘱为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断医嘱是否为明确交代"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// medicalAdviceConditionsPreserved 医嘱保留条件性表述
func medicalAdviceConditionsPreserved(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "medical_advice")
	result.Evidence["field"] = "medical_advice"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "医嘱为空,规则不适用。")
	}

	// 检查是否保留了条件性表述
	conditionalKeywords := []string{"如", "若", "如果", "倘若", "必要时", "严重时", "不缓解"}
	for _, keyword := range conditionalKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "医嘱保留了条件性表述。")
		}
	}

	// 没有条件性表述也是正常的
	return passResult(result, "医嘱已记录。")
}

// ========== 补充 supplement 执行器 ==========

// supplementProperUse 补充栏位合理使用
func supplementProperUse(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "supplement")
	result.Evidence["field"] = "supplement"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "补充栏位为空,规则不适用。")
	}

	// 检查是否误用补充栏(应该记录到其他栏位的内容)
	// 补充栏应该用于记录其他栏位无法容纳的特殊信息
	standardFieldKeywords := []string{"主诉", "现病史", "体格检查", "诊断", "处方"}
	for _, keyword := range standardFieldKeywords {
		if strings.Contains(value, keyword) {
			return failResult(result, "补充栏不应记录标准栏位内容。", "请将内容移至对应栏位。")
		}
	}

	return passResult(result, "补充栏位使用合理。")
}

// supplementNoInvention 补充栏不应编造内容
func supplementNoInvention(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "supplement")
	result.Evidence["field"] = "supplement"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "补充栏位为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断补充内容是否编造"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// ========== 随访计划 followup 执行器 ==========

// followupExplicitPlanOnly 随访计划仅记录明确说明
func followupExplicitPlanOnly(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "followup")
	result.Evidence["field"] = "followup"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "随访计划为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断随访计划是否为明确说明"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// followupDateFormat 随访日期格式规范
func followupDateFormat(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "followup")
	result.Evidence["field"] = "followup"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "随访计划为空,规则不适用。")
	}

	datePattern := regexp.MustCompile(`\d{4}(?:-\d{1,2}-\d{1,2}|/\d{1,2}/\d{1,2}|年\d{1,2}月\d{1,2}日)`)
	match := datePattern.FindString(value)
	if match == "" || !validEMRDate(match) {
		return failResult(result, "复诊日期格式不正确。", "请使用YYYY-MM-DD格式。")
	}

	return passResult(result, "复诊日期格式有效。")
}

func validEMRDate(value string) bool {
	value = strings.TrimSpace(value)
	layouts := []string{"2006-1-2", "2006/1/2", "2006年1月2日"}
	for _, layout := range layouts {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

// followupAmbiguousTimeNotConverted 模糊时间不应转换
func followupAmbiguousTimeNotConverted(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "followup")
	result.Evidence["field"] = "followup"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "随访计划为空,规则不适用。")
	}

	// 此规则需要对比录音判断是否将模糊表述转换为精确日期
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断是否将模糊时间错误转换"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}
