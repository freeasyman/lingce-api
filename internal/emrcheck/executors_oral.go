package emrcheck

import (
	"strings"
)

// ========== 口腔科专用字段执行器 ==========

// oralSpecialtyExamActualFindingsOnly 口腔专科检查仅记录实际发现
func oralSpecialtyExamActualFindingsOnly(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "oral_specialty_exam")
	result.Evidence["field"] = "oral_specialty_exam"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "口腔专科检查为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断是否仅记录实际发现"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// oralToothPositionExplicitPosition 牙位明确标记
func oralToothPositionExplicitPosition(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "oral_tooth_position")
	result.Evidence["field"] = "oral_tooth_position"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "牙位为空,规则不适用。")
	}

	// 检查是否包含牙位标记
	toothMarkers := []string{"#", "牙", "左", "右", "上", "下", "1", "2", "3", "4", "5", "6", "7", "8"}
	hasToothMarker := false
	for _, marker := range toothMarkers {
		if strings.Contains(value, marker) {
			hasToothMarker = true
			break
		}
	}

	if !hasToothMarker {
		return failResult(result, "牙位应使用明确标记。", "请使用牙位号或方位标记(如#16、左上6)。")
	}

	return passResult(result, "牙位标记明确。")
}

// oralToothPositionRelatedContentConsistency 牙位与相关内容一致
func oralToothPositionRelatedContentConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	toothPosition := textValue(data.Content, "oral_tooth_position")
	diagnosis := textValue(data.Content, "diagnosis")
	treatmentPlan := textValue(data.Content, "oral_treatment_plan")

	result.Evidence["oral_tooth_position"] = toothPosition
	result.Evidence["diagnosis"] = diagnosis
	result.Evidence["oral_treatment_plan"] = treatmentPlan

	if strings.TrimSpace(toothPosition) == "" {
		return passResult(result, "牙位为空,规则不适用。")
	}

	// 此规则需要语义判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断牙位与诊断、治疗计划的一致性"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// oralPeriodontalOrDentalConditionExplicitCondition 牙周或牙体状况明确描述
func oralPeriodontalOrDentalConditionExplicitCondition(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "oral_periodontal_or_dental_condition")
	result.Evidence["field"] = "oral_periodontal_or_dental_condition"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "牙周或牙体状况为空,规则不适用。")
	}

	// 检查是否包含具体描述
	conditionKeywords := []string{"龋", "缺损", "松动", "红肿", "出血", "牙石", "牙菌斑", "牙周袋", "正常"}
	for _, condition := range conditionKeywords {
		if strings.Contains(value, condition) {
			return passResult(result, "牙周或牙体状况描述明确。")
		}
	}

	return failResult(result, "牙周或牙体状况应明确描述。", "请描述具体的牙体或牙周状况。")
}

// oralImagingFindingsStatusAndResult 口腔影像检查状态与结果匹配
func oralImagingFindingsStatusAndResult(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "oral_imaging_findings")
	result.Evidence["field"] = "oral_imaging_findings"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "口腔影像检查为空,规则不适用。")
	}

	// 检查是否有状态标记
	if strings.Contains(value, "已完成") || strings.Contains(value, "结果") || strings.Contains(value, "显示") {
		// 检查是否有结果描述
		resultKeywords := []string{"正常", "异常", "未见", "可见", "根尖", "牙槽骨", "密度"}
		for _, keyword := range resultKeywords {
			if strings.Contains(value, keyword) {
				return passResult(result, "影像检查状态与结果匹配。")
			}
		}
		return failResult(result, "已完成的影像检查应记录结果。", "请补充影像检查的发现。")
	}

	return passResult(result, "影像检查已记录。")
}

// oralImagingFindingsNoResultInvention 口腔影像检查不编造结果
func oralImagingFindingsNoResultInvention(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "oral_imaging_findings")
	result.Evidence["field"] = "oral_imaging_findings"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "口腔影像检查为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断影像结果是否编造"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// oralTreatmentPlanExplicitPlan 口腔治疗计划明确记录
func oralTreatmentPlanExplicitPlan(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "oral_treatment_plan")
	result.Evidence["field"] = "oral_treatment_plan"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "口腔治疗计划为空,规则不适用。")
	}

	// 检查是否包含治疗方案
	treatmentKeywords := []string{"充填", "拔除", "根管治疗", "洁治", "刮治", "修复", "种植", "正畸"}
	for _, treatment := range treatmentKeywords {
		if strings.Contains(value, treatment) {
			return passResult(result, "口腔治疗计划明确记录。")
		}
	}

	return failResult(result, "口腔治疗计划应明确具体方案。", "请描述具体的治疗措施。")
}

// oralTreatmentPlanPlannedVsCompleted 口腔治疗计划区分计划与完成
func oralTreatmentPlanPlannedVsCompleted(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "oral_treatment_plan")
	result.Evidence["field"] = "oral_treatment_plan"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "口腔治疗计划为空,规则不适用。")
	}

	// 检查是否区分了状态
	statusKeywords := []string{"计划", "拟", "待", "已完成", "已", "今日"}
	for _, status := range statusKeywords {
		if strings.Contains(value, status) {
			return passResult(result, "口腔治疗计划已区分状态。")
		}
	}

	return failResult(result, "口腔治疗计划应区分计划与已完成。", "请明确标注治疗的完成状态。")
}
