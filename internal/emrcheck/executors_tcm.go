package emrcheck

import (
	"fmt"
	"strings"
)

// ========== 中医专用字段执行器 ==========

// tcmFourExaminationsSourceItemsOnly 中医四诊仅记录来源项
func tcmFourExaminationsSourceItemsOnly(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	// 四诊包括望闻问切的综合
	result.Evidence["field"] = "tcm_four_examinations"

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断四诊是否仅记录来源项"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// tcmFourExaminationsNoTermInvention 中医四诊不编造术语
func tcmFourExaminationsNoTermInvention(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	result.Evidence["field"] = "tcm_four_examinations"

	// 此规则需要对比录音和中医知识判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断四诊术语是否编造"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// tcmInspectionObservationBasis 中医望诊基于观察
func tcmInspectionObservationBasis(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "tcm_inspection")
	result.Evidence["field"] = "tcm_inspection"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "中医望诊为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断望诊是否基于观察"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// tcmAuscultationOlfactionObservationBasis 中医闻诊基于观察
func tcmAuscultationOlfactionObservationBasis(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "tcm_auscultation_olfaction")
	result.Evidence["field"] = "tcm_auscultation_olfaction"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "中医闻诊为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断闻诊是否基于观察"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// tcmInquiryQuestionAnswerFacts 中医问诊记录问答事实
func tcmInquiryQuestionAnswerFacts(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "tcm_inquiry")
	result.Evidence["field"] = "tcm_inquiry"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "中医问诊为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断问诊是否记录问答事实"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// tcmInquiryNoPatternInference 中医问诊不推断证型
func tcmInquiryNoPatternInference(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "tcm_inquiry")
	result.Evidence["field"] = "tcm_inquiry"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "中医问诊为空,规则不适用。")
	}

	// 检查是否在问诊中包含证型判断
	patternKeywords := []string{"证", "型", "虚", "实", "寒", "热", "表", "里"}
	suspiciousCount := 0
	for _, keyword := range patternKeywords {
		if strings.Contains(value, keyword) {
			suspiciousCount++
		}
	}

	// 如果包含多个证型关键词,可能有推断嫌疑
	if suspiciousCount >= 3 {
		return failResult(result, "问诊不应包含证型推断。", "证型判断应在辨证栏位。")
	}

	return passResult(result, "中医问诊记录规范。")
}

// tcmPalpationActualExamBasis 中医切诊基于实际检查
func tcmPalpationActualExamBasis(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "tcm_palpation")
	result.Evidence["field"] = "tcm_palpation"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "中医切诊为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断切诊是否基于实际检查"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// tcmTongueObservationBasis 中医舌诊基于观察
func tcmTongueObservationBasis(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "tcm_tongue")
	result.Evidence["field"] = "tcm_tongue"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "中医舌诊为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断舌诊是否基于观察"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// tcmPulseExplicitObservation 中医脉诊明确记录
func tcmPulseExplicitObservation(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "tcm_pulse")
	result.Evidence["field"] = "tcm_pulse"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "中医脉诊为空,规则不适用。")
	}

	// 检查是否包含脉象描述
	pulseKeywords := []string{"浮", "沉", "迟", "数", "滑", "涩", "弦", "紧", "细", "洪", "缓", "濡"}
	for _, pulse := range pulseKeywords {
		if strings.Contains(value, pulse) {
			return passResult(result, "中医脉诊已明确记录。")
		}
	}

	return failResult(result, "中医脉诊应明确记录脉象。", "请描述具体的脉象特征。")
}

// tcmPatternDifferentiationExplicitPattern 中医辨证明确证型
func tcmPatternDifferentiationExplicitPattern(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "tcm_pattern_differentiation")
	result.Evidence["field"] = "tcm_pattern_differentiation"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "中医辨证为空,规则不适用。")
	}

	// 检查是否包含证型
	if strings.Contains(value, "证") {
		return passResult(result, "中医辨证明确了证型。")
	}

	return failResult(result, "中医辨证应明确证型。", "请明确标注证型(如“气虚证”)。")
}

// tcmPatternDifferentiationFourExaminationsConsistency 中医辨证与四诊一致
func tcmPatternDifferentiationFourExaminationsConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	pattern := textValue(data.Content, "tcm_pattern_differentiation")
	result.Evidence["tcm_pattern_differentiation"] = pattern

	if strings.TrimSpace(pattern) == "" {
		return passResult(result, "中医辨证为空,规则不适用。")
	}

	// 此规则需要中医知识和语义判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断辨证与四诊的一致性"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// tcmDiagnosisExplicitPresent 中医诊断明确存在
func tcmDiagnosisExplicitPresent(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "tcm_diagnosis")
	result.Evidence["field"] = "tcm_diagnosis"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "中医诊断为空。", "请填写中医诊断。")
	}

	return passResult(result, "中医诊断已填写。")
}

// tcmDiagnosisFourExaminationsConsistency 中医诊断与四诊一致
func tcmDiagnosisFourExaminationsConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	diagnosis := textValue(data.Content, "tcm_diagnosis")
	result.Evidence["tcm_diagnosis"] = diagnosis

	if strings.TrimSpace(diagnosis) == "" {
		return passResult(result, "中医诊断为空,规则不适用。")
	}

	// 此规则需要中医知识和语义判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断中医诊断与四诊的一致性"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// tcmTreatmentPrincipleExplicitTreatment 中医治法明确记录
func tcmTreatmentPrincipleExplicitTreatment(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "tcm_treatment_principle")
	result.Evidence["field"] = "tcm_treatment_principle"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "中医治法为空,规则不适用。")
	}

	// 检查是否包含治法
	treatmentKeywords := []string{"补", "泻", "清", "温", "散", "解", "调", "益", "养", "疏"}
	for _, treatment := range treatmentKeywords {
		if strings.Contains(value, treatment) {
			return passResult(result, "中医治法已明确记录。")
		}
	}

	return failResult(result, "中医治法应明确治疗原则。", "请描述具体的治法(如“益气健脾”)。")
}

// tcmTreatmentPrincipleDiagnosisPatternConsistency 中医治法与诊断辨证一致
func tcmTreatmentPrincipleDiagnosisPatternConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	treatment := textValue(data.Content, "tcm_treatment_principle")
	diagnosis := textValue(data.Content, "tcm_diagnosis")
	pattern := textValue(data.Content, "tcm_pattern_differentiation")

	result.Evidence["tcm_treatment_principle"] = treatment
	result.Evidence["tcm_diagnosis"] = diagnosis
	result.Evidence["tcm_pattern_differentiation"] = pattern

	if strings.TrimSpace(treatment) == "" {
		return passResult(result, "中医治法为空,规则不适用。")
	}

	// 此规则需要中医知识判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断治法与诊断、辨证的一致性"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// tcmHerbalPrescriptionKnownItemsComplete checks only the structured facts
// that are actually present in the record. It does not infer missing herbs,
// doses, or usage details from the pattern or treatment principle.
func tcmHerbalPrescriptionKnownItemsComplete(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := data.Content["tcm_herbal_prescription"]
	result.Evidence["field"] = "tcm_herbal_prescription"

	if value == nil || strings.TrimSpace(textValue(data.Content, "tcm_herbal_prescription")) == "" {
		return passResult(result, "中药处方为空,规则不适用。")
	}

	result.Evidence["structured"] = false
	if prescription, ok := value.(map[string]any); ok {
		if nested, ok := prescription["value"].(map[string]any); ok {
			prescription = nested
		}
		result.Evidence["structured"] = true
		fields := parameterStrings(rule.Parameters, "fields")
		missing := make([]string, 0)
		for _, field := range fields {
			raw, exists := prescription[field]
			if !exists || strings.TrimSpace(fmt.Sprint(raw)) == "" || fmt.Sprint(raw) == "<nil>" {
				missing = append(missing, field)
			}
		}
		result.Evidence["missing_fields"] = missing
		if len(missing) > 0 {
			return failResult(result, "中药处方中已结构化的项目不完整。", "请补充已记录药材对应的剂量、用法、频次或疗程。")
		}
	}

	return passResult(result, "中药处方已保留当前记录的信息。")
}

// tcmHerbalPrescriptionNoFormulaInvention 中药处方不编造方剂
func tcmHerbalPrescriptionNoFormulaInvention(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "tcm_herbal_prescription")
	result.Evidence["field"] = "tcm_herbal_prescription"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "中药处方为空,规则不适用。")
	}

	// 此规则需要对比录音和中医知识判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断方剂是否编造"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// tcmHerbalPrescriptionPatternTreatmentConsistency 中药处方与辨证治法一致
func tcmHerbalPrescriptionPatternTreatmentConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	prescription := textValue(data.Content, "tcm_herbal_prescription")
	pattern := textValue(data.Content, "tcm_pattern_differentiation")
	treatment := textValue(data.Content, "tcm_treatment_principle")

	result.Evidence["tcm_herbal_prescription"] = prescription
	result.Evidence["tcm_pattern_differentiation"] = pattern
	result.Evidence["tcm_treatment_principle"] = treatment

	if strings.TrimSpace(prescription) == "" {
		return passResult(result, "中药处方为空,规则不适用。")
	}

	// 此规则需要中医知识判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断处方与辨证治法的一致性"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}
