package emrcheck

import (
	"regexp"
	"strings"
)

// ========== 妇科专用字段执行器 ==========

// lastMenstrualPeriodValidDate 末次月经日期有效
func lastMenstrualPeriodValidDate(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "last_menstrual_period")
	result.Evidence["field"] = "last_menstrual_period"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "末次月经为空,规则不适用。")
	}

	datePattern := regexp.MustCompile(`^\s*(\d{4}(?:-\d{1,2}-\d{1,2}|/\d{1,2}/\d{1,2}|年\d{1,2}月\d{1,2}日))\s*$`)
	if datePattern.MatchString(value) && validEMRDate(strings.TrimSpace(value)) {
		return passResult(result, "末次月经日期格式有效。")
	}

	// 检查是否为明确否认
	if normalized := strings.TrimSpace(value); normalized == "不详" || normalized == "记不清" {
		return passResult(result, "末次月经明确标注不详。")
	}

	return failResult(result, "末次月经应填写有效日期或明确“不详”。", "请填写真实日期，或填写“不详”“记不清”。")
}

// lastMenstrualPeriodNoAmbiguousConversion 末次月经模糊表述不转换
func lastMenstrualPeriodNoAmbiguousConversion(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "last_menstrual_period")
	result.Evidence["field"] = "last_menstrual_period"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "末次月经为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断是否将模糊表述转换为精确日期"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// menstrualHistoryKnownItemsRecorded 月经史已知项目记录
func menstrualHistoryKnownItemsRecorded(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "menstrual_history")
	result.Evidence["field"] = "menstrual_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "月经史为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断已知项目是否记录"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// menstrualHistoryNoInference 月经史不应推断
func menstrualHistoryNoInference(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "menstrual_history")
	result.Evidence["field"] = "menstrual_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "月经史为空,规则不适用。")
	}

	// 此规则需要模型判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断月经史是否为推断"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// maritalReproductiveHistoryKnownItemsRecorded 婚育史已知项目记录
func maritalReproductiveHistoryKnownItemsRecorded(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "marital_reproductive_history")
	result.Evidence["field"] = "marital_reproductive_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "婚育史为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断已知项目是否记录"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// maritalReproductiveHistoryNoInference 婚育史不应推断
func maritalReproductiveHistoryNoInference(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "marital_reproductive_history")
	result.Evidence["field"] = "marital_reproductive_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "婚育史为空,规则不适用。")
	}

	// 此规则需要模型判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断婚育史是否为推断"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// obstetricHistoryExplicitFactsOnly 产史仅记录明确事实
func obstetricHistoryExplicitFactsOnly(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "obstetric_history")
	result.Evidence["field"] = "obstetric_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "产史为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断是否仅记录明确事实"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// obstetricHistoryNumbersFormat 产史数字格式规范(G1P1)
func obstetricHistoryNumbersFormat(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "obstetric_history")
	result.Evidence["field"] = "obstetric_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "产史为空,规则不适用。")
	}

	gpPattern := regexp.MustCompile(`^\s*G\d+P\d+(?:A\d+)?(?:L\d+)?\s*$`)
	if gpPattern.MatchString(value) {
		return passResult(result, "产史数字格式规范。")
	}

	chinesePattern := regexp.MustCompile(`^\s*孕\d+\s*产\d+(?:\s*流产\d+)?(?:\s*存活\d+)?\s*$`)
	if chinesePattern.MatchString(value) {
		return passResult(result, "产史已记录。")
	}

	return failResult(result, "孕产史数量格式不正确。", "请使用G1P1或孕1产1格式。")
}

// gynecologySpecialtyExamCompletedExamOnly 妇科专科检查仅记录已完成
func gynecologySpecialtyExamCompletedExamOnly(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "gynecology_specialty_exam")
	result.Evidence["field"] = "gynecology_specialty_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "妇科专科检查为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断是否仅记录已完成检查"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// gynecologySpecialtyExamNoUnexaminedNormal 妇科专科检查未检不记正常
func gynecologySpecialtyExamNoUnexaminedNormal(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "gynecology_specialty_exam")
	result.Evidence["field"] = "gynecology_specialty_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "妇科专科检查为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断未检查部位是否误记为正常"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// gynecologyAuxiliaryExamStatusAndResult 妇科辅助检查状态与结果匹配
func gynecologyAuxiliaryExamStatusAndResult(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "gynecology_auxiliary_exam")
	result.Evidence["field"] = "gynecology_auxiliary_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "妇科辅助检查为空,规则不适用。")
	}

	// 复用通用辅助检查逻辑
	return auxiliaryExamResultForCompleted(rule, data)
}

// gynecologyAuxiliaryExamNoResultInvention 妇科辅助检查不编造结果
func gynecologyAuxiliaryExamNoResultInvention(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "gynecology_auxiliary_exam")
	result.Evidence["field"] = "gynecology_auxiliary_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "妇科辅助检查为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断检查结果是否编造"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}
