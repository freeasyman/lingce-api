package emrcheck

import (
	"regexp"
	"strings"
)

// ========== 儿科专用字段执行器 ==========

// pediatricBirthHistoryKnownItemsRecorded 出生史已知项目记录
func pediatricBirthHistoryKnownItemsRecorded(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "pediatric_birth_history")
	result.Evidence["field"] = "pediatric_birth_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "出生史为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断已知项目是否记录"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// pediatricBirthHistoryNoInference 出生史不应推断
func pediatricBirthHistoryNoInference(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "pediatric_birth_history")
	result.Evidence["field"] = "pediatric_birth_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "出生史为空,规则不适用。")
	}

	// 此规则需要模型判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断出生史是否为推断"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// pediatricFeedingHistoryKnownItemsRecorded 喂养史已知项目记录
func pediatricFeedingHistoryKnownItemsRecorded(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "pediatric_feeding_history")
	result.Evidence["field"] = "pediatric_feeding_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "喂养史为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断已知项目是否记录"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// pediatricFeedingHistoryNoInference 喂养史不应推断
func pediatricFeedingHistoryNoInference(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "pediatric_feeding_history")
	result.Evidence["field"] = "pediatric_feeding_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "喂养史为空,规则不适用。")
	}

	// 此规则需要模型判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断喂养史是否为推断"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// pediatricGuardianStatementSourceDistinction 监护人陈述区分来源
func pediatricGuardianStatementSourceDistinction(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "pediatric_guardian_statement")
	result.Evidence["field"] = "pediatric_guardian_statement"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "监护人陈述为空,规则不适用。")
	}

	// 检查是否标明了陈述来源
	sourceKeywords := []string{"母亲", "父亲", "家长", "爷爷", "奶奶", "外公", "外婆", "监护人"}
	for _, source := range sourceKeywords {
		if strings.Contains(value, source) {
			return passResult(result, "监护人陈述已标明来源。")
		}
	}

	return failResult(result, "监护人陈述应标明来源。", "请明确是谁提供的信息(如“母亲述”)。")
}

// pediatricGuardianStatementNoInference 监护人陈述不应推断
func pediatricGuardianStatementNoInference(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "pediatric_guardian_statement")
	result.Evidence["field"] = "pediatric_guardian_statement"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "监护人陈述为空,规则不适用。")
	}

	// 此规则需要模型判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断监护人陈述是否为推断"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// pediatricWeightValueAndUnit 儿童体重数值与单位
func pediatricWeightValueAndUnit(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "pediatric_weight")
	result.Evidence["field"] = "pediatric_weight"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "儿童体重为空,规则不适用。")
	}

	weightPattern := regexp.MustCompile(`(?i)^\s*\d+(?:\.\d+)?\s*(kg|公斤|千克)\s*$`)
	if weightPattern.MatchString(value) {
		return passResult(result, "儿童体重格式规范。")
	}

	return failResult(result, "儿童体重格式不正确。", "请填写体重数值和单位，例如15公斤；不能使用“斤”。")
}

// pediatricWeightNoAgeEstimation 儿童体重不应根据年龄估算
func pediatricWeightNoAgeEstimation(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "pediatric_weight")
	result.Evidence["field"] = "pediatric_weight"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "儿童体重为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断体重是否为估算"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// pediatricWeightMedicationBasisConsistency 儿童体重与用药依据一致
func pediatricWeightMedicationBasisConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	weight := textValue(data.Content, "pediatric_weight")
	medicationBasis := textValue(data.Content, "pediatric_medication_basis")

	result.Evidence["pediatric_weight"] = weight
	result.Evidence["pediatric_medication_basis"] = medicationBasis

	if strings.TrimSpace(weight) == "" || strings.TrimSpace(medicationBasis) == "" {
		return passResult(result, "儿童体重或用药依据为空,规则不适用。")
	}

	// 提取体重数值
	weightPattern := regexp.MustCompile(`(\d+(\.\d+)?)\s*(kg|公斤|千克)`)
	matches := weightPattern.FindStringSubmatch(weight)
	if len(matches) == 0 {
		return failResult(result, "无法提取儿童体重数值。", "请规范体重格式。")
	}

	weightValue := matches[1]

	// 检查用药依据中是否使用了相同体重
	if strings.Contains(medicationBasis, weightValue) {
		return passResult(result, "儿童体重与用药依据一致。")
	}

	return failResult(result, "用药依据中的体重与记录的体重不一致。", "请确保用药计算使用正确的体重。")
}

// pediatricMedicationBasisExplicitBasisRecorded 儿童用药依据明确记录
func pediatricMedicationBasisExplicitBasisRecorded(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "pediatric_medication_basis")
	result.Evidence["field"] = "pediatric_medication_basis"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "儿童用药依据为空,规则不适用。")
	}

	// 检查是否包含用药依据(体重、年龄等)
	basisKeywords := []string{"kg", "公斤", "岁", "月龄", "体重"}
	for _, basis := range basisKeywords {
		if strings.Contains(value, basis) {
			return passResult(result, "儿童用药依据已明确记录。")
		}
	}

	return failResult(result, "儿童用药应明确记录依据。", "请记录用药计算的依据(体重或年龄)。")
}

// pediatricMedicationBasisPrescriptionConsistency 儿童用药依据与处方一致
func pediatricMedicationBasisPrescriptionConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	medicationBasis := textValue(data.Content, "pediatric_medication_basis")
	prescription := textValue(data.Content, "prescription")

	result.Evidence["pediatric_medication_basis"] = medicationBasis
	result.Evidence["prescription"] = prescription

	if strings.TrimSpace(medicationBasis) == "" || strings.TrimSpace(prescription) == "" {
		return passResult(result, "儿童用药依据或处方为空,规则不适用。")
	}

	// 此规则需要深度语义判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断用药依据与处方剂量的一致性"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}
