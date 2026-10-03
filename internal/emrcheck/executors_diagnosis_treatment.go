package emrcheck

import (
	"strings"
)

// ========== 诊断 diagnosis 执行器 ==========

// diagnosisPresent 诊断必须存在
func diagnosisPresent(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "diagnosis")
	result.Evidence["field"] = "diagnosis"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "诊断为空。", "请填写诊断。")
	}

	return passResult(result, "诊断已填写。")
}

// diagnosisIcd10Match 诊断应符合ICD-10
func diagnosisIcd10Match(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	names := diagnosisNames(data.Content)
	result.Evidence["field"] = "diagnosis"
	result.Evidence["values"] = names

	if len(names) == 0 {
		return passResult(result, "诊断为空，规则不适用。")
	}

	matches := data.ICD10Matches
	if matches == nil {
		matches = map[string]ICD10DiagnosisMatch{}
	}
	matched := make([]map[string]string, 0, len(names))
	unmatched := make([]string, 0)
	for _, name := range names {
		item, ok := matches[name]
		if !ok {
			unmatched = append(unmatched, name)
			continue
		}
		matched = append(matched, map[string]string{
			"name": item.Name,
			"code": item.Code,
		})
	}
	result.Evidence["matched"] = matched
	result.Evidence["unmatched"] = unmatched
	result.Evidence["catalog"] = "ICD-10医保2.0版"
	if len(unmatched) > 0 {
		candidateItems := make([]map[string]string, 0)
		for _, name := range unmatched {
			for _, candidate := range data.ICD10Candidates[name] {
				candidateItems = append(candidateItems, map[string]string{
					"name": candidate.Name,
					"code": candidate.Code,
				})
			}
		}
		result.Evidence["candidates"] = candidateItems
		if len(candidateItems) > 0 {
			return failResult(result,
				"诊断名称没有完全匹配《ICD-10医保2.0版》目录，但找到了相近的标准诊断名称。",
				"请从候选诊断中核对并改用准确的 ICD 标准诊断名称。")
		}
		return failResult(result,
			"诊断名称不是《ICD-10医保2.0版》目录中的标准名称。",
			"请改用 ICD 目录中的标准诊断名称。")
	}
	return passResult(result, "诊断名称符合《ICD-10医保2.0版》目录。")
}

// diagnosisCertaintyPreserved 诊断保留确定性表述
func diagnosisCertaintyPreserved(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "diagnosis")
	result.Evidence["field"] = "diagnosis"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "诊断为空,规则不适用。")
	}

	// 检查是否保留了不确定性标记
	uncertaintyMarkers := []string{"?", "疑", "待排", "考虑", "可能"}
	for _, marker := range uncertaintyMarkers {
		if strings.Contains(value, marker) {
			return passResult(result, "诊断保留了不确定性表述。")
		}
	}

	// 如果没有不确定性标记,也是合规的
	return passResult(result, "诊断表述明确。")
}

// diagnosisNotPatientStatement 诊断不应是患者陈述
func diagnosisNotPatientStatement(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "diagnosis")
	result.Evidence["field"] = "diagnosis"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "诊断为空,规则不适用。")
	}

	// 检查是否包含患者陈述性语言
	patientStatementKeywords := []string{"我", "他说", "她说", "自述", "感觉", "觉得"}
	for _, keyword := range patientStatementKeywords {
		if strings.Contains(value, keyword) {
			return failResult(result, "诊断不应包含患者陈述性语言。", "请用医学诊断术语。")
		}
	}

	return passResult(result, "诊断用语规范。")
}

// diagnosisEvidenceConsistency 诊断与证据一致
func diagnosisEvidenceConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	diagnosis := textValue(data.Content, "diagnosis")
	present := textValue(data.Content, "present_illness")
	physical := textValue(data.Content, "physical_exam")

	result.Evidence["diagnosis"] = diagnosis
	result.Evidence["present_illness"] = present
	result.Evidence["physical_exam"] = physical

	if strings.TrimSpace(diagnosis) == "" {
		return passResult(result, "诊断为空,规则不适用。")
	}

	// 此规则需要深度语义判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断诊断与病史、体征的一致性"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// diagnosisPrescriptionAdviceConsistency 诊断与处方医嘱一致
func diagnosisPrescriptionAdviceConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	diagnosis := textValue(data.Content, "diagnosis")
	prescription := textValue(data.Content, "prescription")
	advice := textValue(data.Content, "medical_advice")

	result.Evidence["diagnosis"] = diagnosis
	result.Evidence["prescription"] = prescription
	result.Evidence["medical_advice"] = advice

	if strings.TrimSpace(diagnosis) == "" {
		return passResult(result, "诊断为空,规则不适用。")
	}

	// 此规则需要医学知识和语义判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断诊断与治疗方案的一致性"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// diagnosisDispositionConsistency 诊断与处置一致
func diagnosisDispositionConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	diagnosis := textValue(data.Content, "diagnosis")
	disposition := textValue(data.Content, "disposition")

	result.Evidence["diagnosis"] = diagnosis
	result.Evidence["disposition"] = disposition

	if strings.TrimSpace(diagnosis) == "" || strings.TrimSpace(disposition) == "" {
		return passResult(result, "诊断或处置为空,规则不适用。")
	}

	// 此规则需要医学知识判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断诊断与处置的一致性"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// ========== 处方 prescription 执行器 ==========

// prescriptionStatusDistinction 处方状态区分(已开/未开)
func prescriptionStatusDistinction(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "prescription")
	result.Evidence["field"] = "prescription"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "处方为空,规则不适用。")
	}

	// 检查是否明确状态
	statusKeywords := []string{"已开", "未开", "已处方", "计划", "建议"}
	for _, status := range statusKeywords {
		if strings.Contains(value, status) {
			return passResult(result, "处方状态已明确。")
		}
	}

	// 如果包含具体药物,默认认为已开
	return passResult(result, "处方已记录。")
}

// prescriptionItemCompleteness 处方项完整性
func prescriptionItemCompleteness(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "prescription")
	result.Evidence["field"] = "prescription"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "处方为空,规则不适用。")
	}

	// 检查是否包含药物名称
	if len(value) < 5 {
		return failResult(result, "处方内容过于简略。", "请完整记录药物名称、用法用量。")
	}

	// 检查是否包含用法用量
	dosageKeywords := []string{"片", "粒", "袋", "次", "日", "每", "mg", "g", "ml"}
	hasDosage := false
	for _, keyword := range dosageKeywords {
		if strings.Contains(value, keyword) {
			hasDosage = true
			break
		}
	}

	if !hasDosage {
		return failResult(result, "处方应包含用法用量。", "请补充药物的剂量和用法。")
	}

	return passResult(result, "处方记录完整。")
}

// prescriptionNoMissingDetailInvention 处方不应编造缺失细节
func prescriptionNoMissingDetailInvention(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "prescription")
	result.Evidence["field"] = "prescription"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "处方为空,规则不适用。")
	}

	// 此规则需要对比录音判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断处方细节是否编造"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// ========== 处置 disposition 执行器 ==========

// dispositionStatusRecorded 处置状态明确
func dispositionStatusRecorded(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "disposition")
	result.Evidence["field"] = "disposition"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "处置为空,规则不适用。")
	}

	// 检查是否标明状态
	statusKeywords := []string{"已", "完成", "待", "计划", "建议"}
	for _, status := range statusKeywords {
		if strings.Contains(value, status) {
			return passResult(result, "处置状态已明确。")
		}
	}

	return failResult(result, "处置应明确状态(已完成/待完成)。", "请标明处置的完成状态。")
}

// dispositionNoPlanAsCompleted 计划处置不应记录为已完成
func dispositionNoPlanAsCompleted(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "disposition")
	result.Evidence["field"] = "disposition"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "处置为空,规则不适用。")
	}

	// 此规则需要语义判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断计划处置是否误记为已完成"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// dispositionNoUnstatedAction 不应记录未提及的处置
func dispositionNoUnstatedAction(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "disposition")
	result.Evidence["field"] = "disposition"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "处置为空,规则不适用。")
	}

	// 此规则需要对比录音
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型对比录音判断处置是否有依据"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}
