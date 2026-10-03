package emrcheck

import (
	"regexp"
	"strings"
)

// ========== 体格检查 physical_exam 执行器 ==========

// physicalExamVitalSignsFormat 生命体征格式规范
func physicalExamVitalSignsFormat(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "physical_exam")
	result.Evidence["field"] = "physical_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "体格检查为空。", "请填写体格检查。")
	}

	// 允许医生只输入体温数值；单位不是提交门槛，避免把键盘输入
	// 不便的符号当成病历质量问题。
	tempPattern := regexp.MustCompile(`\d+(?:\.\d+)?(?:[℃°C])?`)
	if strings.Contains(value, "体温") || strings.Contains(value, "T:") {
		if !tempPattern.MatchString(value) {
			return failResult(result, "体温没有记录有效数值。", "请补充体温数值。")
		}
	}

	// 检查血压格式 (120/80mmHg)
	bpPattern := regexp.MustCompile(`\d+/\d+\s*mmHg`)
	if strings.Contains(value, "血压") || strings.Contains(value, "BP:") {
		if !bpPattern.MatchString(value) {
			return failResult(result, "血压应包含单位mmHg。", "请使用格式:血压 120/80mmHg")
		}
	}

	// 检查脉搏/心率格式
	pulsePattern := regexp.MustCompile(`\d+\s*次/分`)
	if strings.Contains(value, "脉搏") || strings.Contains(value, "心率") || strings.Contains(value, "P:") {
		if !pulsePattern.MatchString(value) {
			return failResult(result, "脉搏/心率应包含单位次/分。", "请使用格式:心率 80次/分")
		}
	}

	return passResult(result, "生命体征格式规范。")
}

// physicalExamActualExamBasis 体格检查基于实际检查
func physicalExamActualExamBasis(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "physical_exam")
	result.Evidence["field"] = "physical_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "体格检查为空。", "请填写体格检查。")
	}

	// 此规则需要对比录音判断是否真实检查
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断是否实际检查"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// physicalExamNoUnexaminedNormal 未检查部位不应记录为正常
func physicalExamNoUnexaminedNormal(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "physical_exam")
	result.Evidence["field"] = "physical_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "体格检查为空,规则不适用。")
	}

	// 此规则需要对比录音判断哪些部位未检查
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断未检查部位是否误记为正常"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// physicalExamPositiveFindingsEvidence 阳性体征需有录音依据
func physicalExamPositiveFindingsEvidence(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "physical_exam")
	result.Evidence["field"] = "physical_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "体格检查为空,规则不适用。")
	}

	// 此规则需要对比录音判断阳性体征是否有依据
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断阳性体征是否有依据"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// ========== 辅助检查 auxiliary_exam 执行器 ==========

// auxiliaryExamItemStatusRecorded 辅助检查状态明确记录
func auxiliaryExamItemStatusRecorded(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "auxiliary_exam")
	result.Evidence["field"] = "auxiliary_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "辅助检查为空,规则不适用。")
	}

	// 检查是否有状态标记
	statusKeywords := []string{"已完成", "待完成", "未做", "结果", "报告", "显示"}
	for _, status := range statusKeywords {
		if strings.Contains(value, status) {
			return passResult(result, "辅助检查状态已记录。")
		}
	}

	// 如果只是简单列举,没有状态
	examKeywords := []string{"血常规", "尿常规", "X光", "CT", "B超", "心电图"}
	hasExamItem := false
	for _, exam := range examKeywords {
		if strings.Contains(value, exam) {
			hasExamItem = true
			break
		}
	}

	if hasExamItem {
		return failResult(result, "辅助检查应明确状态(已完成/待完成)。", "请标明每项检查的完成状态。")
	}

	return passResult(result, "辅助检查已记录。")
}

// auxiliaryExamResultForCompleted 已完成的检查应有结果
func auxiliaryExamResultForCompleted(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "auxiliary_exam")
	result.Evidence["field"] = "auxiliary_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "辅助检查为空,规则不适用。")
	}

	// 检查是否标记为已完成
	if !strings.Contains(value, "已完成") && !strings.Contains(value, "结果") && !strings.Contains(value, "显示") {
		return passResult(result, "未涉及已完成检查,规则不适用。")
	}

	// 检查是否有结果描述
	resultKeywords := []string{"正常", "异常", "未见", "可见", "增高", "降低", "阳性", "阴性", "数值"}
	for _, keyword := range resultKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "已完成检查记录了结果。")
		}
	}

	return failResult(result, "已完成的检查应记录结果。", "请补充检查结果或异常发现。")
}

// auxiliaryExamPendingExplicit 待完成检查明确标注
func auxiliaryExamPendingExplicit(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "auxiliary_exam")
	result.Evidence["field"] = "auxiliary_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "辅助检查为空,规则不适用。")
	}

	// 检查是否有待完成的标记
	if strings.Contains(value, "待完成") || strings.Contains(value, "待查") || strings.Contains(value, "计划") {
		return passResult(result, "待完成检查已明确标注。")
	}

	// 如果有检查项但没有结果,可能是待完成但未标注
	examKeywords := []string{"血常规", "尿常规", "X光", "CT", "B超", "心电图"}
	hasExamItem := false
	for _, exam := range examKeywords {
		if strings.Contains(value, exam) {
			hasExamItem = true
			break
		}
	}

	if hasExamItem {
		// 检查是否有结果
		resultKeywords := []string{"结果", "显示", "正常", "异常", "未见", "可见"}
		hasResult := false
		for _, keyword := range resultKeywords {
			if strings.Contains(value, keyword) {
				hasResult = true
				break
			}
		}

		if !hasResult {
			return failResult(result, "未完成的检查应标注“待完成”。", "请明确标注待完成检查。")
		}
	}

	return passResult(result, "辅助检查记录规范。")
}

// auxiliaryExamNoPlanAsResult 计划检查不应记录为结果
func auxiliaryExamNoPlanAsResult(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "auxiliary_exam")
	result.Evidence["field"] = "auxiliary_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "辅助检查为空,规则不适用。")
	}

	// 此规则需要语义判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断是否将计划检查误记为已完成"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}
