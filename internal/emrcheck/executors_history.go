package emrcheck

import (
	"fmt"
	"strings"
)

// ========== 既往史 past_history 执行器 ==========

// pastHistoryRecordedOrExplicitlyDenied 既往史记录具体疾病或明确否认
func pastHistoryRecordedOrExplicitlyDenied(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "past_history")
	result.Evidence["field"] = "past_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "既往史为空。", "请记录既往疾病史或明确否认。")
	}

	// 检查是否明确否认
	denyKeywords := []string{"无", "否认", "未", "健康", "无特殊"}
	for _, keyword := range denyKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "既往史已明确否认或记录。")
		}
	}

	// 检查是否包含疾病名称
	diseaseKeywords := []string{"病", "炎", "症", "瘤", "癌", "手术", "外伤", "高血压", "糖尿病"}
	for _, keyword := range diseaseKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "既往史记录了具体疾病。")
		}
	}

	return failResult(result, "既往史应记录具体疾病或明确否认。", "请明确记录既往疾病或写“无特殊”。")
}

// pastHistoryNotCurrentIllness 既往史不应包含本次疾病
func pastHistoryNotCurrentIllness(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	past := textValue(data.Content, "past_history")
	present := textValue(data.Content, "present_illness")

	result.Evidence["past_history"] = past
	result.Evidence["present_illness"] = present

	if strings.TrimSpace(past) == "" {
		return passResult(result, "既往史为空,规则不适用。")
	}

	// 此规则需要语义理解,程序难以准确判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断既往史是否误录本次疾病"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// ========== 个人史 personal_history 执行器 ==========

// personalHistoryRecordedWhenMentioned 个人史提及即应记录
func personalHistoryRecordedWhenMentioned(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "personal_history")
	result.Evidence["field"] = "personal_history"
	result.Evidence["value"] = value

	// 如果录音中提到个人史相关内容,应该记录
	// 但程序无法获取录音内容,此规则需要模型判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要对比录音和病历判断是否遗漏"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// personalHistoryNoInference 个人史不应推断
func personalHistoryNoInference(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "personal_history")
	result.Evidence["field"] = "personal_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "个人史为空,规则不适用。")
	}

	// 此规则需要模型判断是否为推断内容
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断个人史是否为推断"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// ========== 过敏史 allergy_history 执行器 ==========

// allergyHistoryStatusValid 过敏史状态合法
func allergyHistoryStatusValid(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	result.Evidence["field"] = "allergy_history"
	status, allergy := allergyStatus(data.Content)
	result.Evidence["status"] = status
	result.Evidence["value"] = allergy

	allowed := parameterStrings(rule.Parameters, "allowed_statuses")
	if strings.TrimSpace(status) == "" {
		return failResult(result, "过敏史状态未填写。", "请明确填写过敏史。")
	}
	for _, item := range allowed {
		if status == item {
			return passResult(result, "过敏史状态已记录。")
		}
	}

	return failResult(result, "过敏史状态不在允许范围内。", "请重新选择过敏史状态。")
}

// allergyHistoryNoneEvidence 否认过敏需有依据
func allergyHistoryNoneEvidence(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "allergy_history")
	result.Evidence["field"] = "allergy_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "过敏史为空,规则不适用。")
	}

	// 检查是否否认过敏
	if strings.Contains(value, "无") || strings.Contains(value, "否认") {
		// 此规则需要对比录音确认是否真实询问
		result.ExecutionStatus = "incomplete"
		result.IncompleteReason = "需要模型确认录音中是否真实询问过敏史"
		result.Conclusion = "未判断"
		result.CheckStatus = "未判断"
		result.HandlingResult = "跳过"
		return result
	}

	return passResult(result, "过敏史记录了具体过敏物。")
}

// allergyHistoryUnconfirmed 不确定的过敏标记为"待证实"
func allergyHistoryUnconfirmed(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	result.Evidence["field"] = "allergy_history"
	status, allergy := allergyStatus(data.Content)
	result.Evidence["status"] = status
	result.Evidence["value"] = allergy
	incomplete := parameterStrings(rule.Parameters, "incomplete_statuses")
	for _, item := range incomplete {
		if status == item {
			return failResult(result, "过敏史尚未完成确认。", "请完成过敏史询问和确认。")
		}
	}
	return passResult(result, "过敏史已完成确认。")
}

// allergyHistoryDrugDetail 药物过敏记录详细
func allergyHistoryDrugDetail(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	result.Evidence["field"] = "allergy_history"
	status, allergy := allergyStatus(data.Content)
	result.Evidence["status"] = status
	result.Evidence["value"] = allergy
	expected := parameterString(rule.Parameters, "required_when_status")
	key := parameterString(rule.Parameters, "required_key")
	if status != expected {
		return passResult(result, "当前过敏史状态不要求填写药物过敏原。")
	}
	detail := strings.TrimSpace(fmt.Sprint(allergy[key]))
	result.Evidence["detail"] = detail
	if detail != "" && detail != "<nil>" {
		return passResult(result, "具体药物过敏原已记录。")
	}
	return failResult(result, "具体药物过敏原未填写。", "请补充具体药物名称。")
}

// allergyHistoryOtherDetail 其他过敏记录详细
func allergyHistoryOtherDetail(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	result.Evidence["field"] = "allergy_history"
	status, allergy := allergyStatus(data.Content)
	result.Evidence["status"] = status
	result.Evidence["value"] = allergy
	expected := parameterString(rule.Parameters, "required_when_status")
	key := parameterString(rule.Parameters, "required_key")
	if status != expected {
		return passResult(result, "当前过敏史状态不要求填写其他过敏原。")
	}
	detail := strings.TrimSpace(fmt.Sprint(allergy[key]))
	result.Evidence["detail"] = detail
	if detail != "" && detail != "<nil>" {
		return passResult(result, "其他过敏原已记录。")
	}
	return failResult(result, "其他过敏原未填写。", "请补充具体过敏原。")
}

func parameterString(parameters map[string]any, key string) string {
	if value, ok := parameters[key]; ok && value != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}

func parameterStrings(parameters map[string]any, key string) []string {
	value, ok := parameters[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if item != nil {
				items = append(items, strings.TrimSpace(fmt.Sprint(item)))
			}
		}
		return items
	case []string:
		return typed
	default:
		return []string{strings.TrimSpace(fmt.Sprint(value))}
	}
}

// allergyHistoryPrescriptionConsistency 过敏史与处方一致
func allergyHistoryPrescriptionConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	allergy := textValue(data.Content, "allergy_history")
	prescription := textValue(data.Content, "prescription")

	result.Evidence["allergy_history"] = allergy
	result.Evidence["prescription"] = prescription

	if strings.TrimSpace(allergy) == "" || strings.TrimSpace(prescription) == "" {
		return passResult(result, "过敏史或处方为空,规则不适用。")
	}

	// 此规则需要药物知识和语义理解
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断处方药物是否与过敏史冲突"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// ========== 家族史 family_history 执行器 ==========

// familyHistoryRecordedOrExplicitlyDenied 家族史记录或明确否认
func familyHistoryRecordedOrExplicitlyDenied(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "family_history")
	result.Evidence["field"] = "family_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "家族史为空。", "请记录家族史或明确否认。")
	}

	// 检查是否明确否认
	denyKeywords := []string{"无", "否认", "未", "无特殊"}
	for _, keyword := range denyKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "家族史已明确否认或记录。")
		}
	}

	// 检查是否包含疾病或亲属关系
	contentKeywords := []string{"父", "母", "兄", "弟", "姐", "妹", "病", "癌", "高血压", "糖尿病"}
	for _, keyword := range contentKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "家族史记录了具体内容。")
		}
	}

	return failResult(result, "家族史应记录具体疾病或写“无特殊”。", "请明确记录家族疾病史或写“无特殊”。")
}

// familyHistoryNoInference 家族史不应推断
func familyHistoryNoInference(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "family_history")
	result.Evidence["field"] = "family_history"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "家族史为空,规则不适用。")
	}

	// 此规则需要模型判断是否为推断内容
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断家族史是否为推断"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}
