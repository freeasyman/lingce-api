package emrcheck

import (
	"strings"
)

// ========== 现病史 present_illness 执行器 ==========

// presentIllnessOnset 现病史包含起病情况
func presentIllnessOnset(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "present_illness")
	result.Evidence["field"] = "present_illness"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "现病史为空。", "请填写现病史。")
	}

	// 检查起病相关关键词
	onsetKeywords := []string{"前", "开始", "起病", "出现", "发生", "发作", "感", "自觉", "无明显诱因"}
	for _, keyword := range onsetKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "现病史包含起病情况描述。")
		}
	}

	return failResult(result, "现病史应包含起病时间和诱因。", "请描述症状何时开始出现。")
}

// presentIllnessEvolution 现病史包含病情演变
func presentIllnessEvolution(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "present_illness")
	result.Evidence["field"] = "present_illness"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "现病史为空。", "请填写现病史。")
	}

	// 检查演变相关关键词
	evolutionKeywords := []string{"后", "逐渐", "渐", "加重", "减轻", "缓解", "好转", "恶化", "持续", "反复", "波动", "变化"}
	for _, keyword := range evolutionKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "现病史包含病情演变描述。")
		}
	}

	return failResult(result, "现病史应包含病情演变过程。", "请描述症状的变化趋势。")
}

// presentIllnessAssociatedSymptoms 现病史记录伴随症状
func presentIllnessAssociatedSymptoms(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "present_illness")
	result.Evidence["field"] = "present_illness"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "现病史为空。", "请填写现病史。")
	}

	// 检查伴随症状标记词
	associatedKeywords := []string{"伴", "伴有", "同时", "并", "兼有"}
	for _, keyword := range associatedKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "现病史记录了伴随症状。")
		}
	}

	// 如果没有明确的"伴"字,但病史较长,也认为可能包含
	if len(value) > 100 {
		return passResult(result, "现病史已详细记录。")
	}

	return failResult(result, "现病史应记录伴随症状。", "如有伴随症状,请使用“伴”字明确标注。")
}

// presentIllnessPriorTreatment 现病史记录既往治疗
func presentIllnessPriorTreatment(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "present_illness")
	result.Evidence["field"] = "present_illness"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "现病史为空。", "请填写现病史。")
	}

	// 检查治疗相关关键词
	treatmentKeywords := []string{"治疗", "服", "用药", "就诊", "诊治", "予以", "给予", "口服", "静脉", "注射", "输液", "检查"}
	for _, keyword := range treatmentKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "现病史记录了既往治疗情况。")
		}
	}

	// 检查是否明确说明未治疗
	if strings.Contains(value, "未") && (strings.Contains(value, "治疗") || strings.Contains(value, "就诊")) {
		return passResult(result, "现病史明确说明未治疗。")
	}

	return failResult(result, "现病史应记录既往治疗情况或明确说明未治疗。", "请补充患者是否接受过治疗。")
}

// presentIllnessTreatmentEffect 记录既往治疗的疗效
func presentIllnessTreatmentEffect(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "present_illness")
	result.Evidence["field"] = "present_illness"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "现病史为空。", "请填写现病史。")
	}

	// 先检查是否提到治疗
	hasTreatment := false
	treatmentKeywords := []string{"治疗", "服", "用药", "予以", "给予"}
	for _, keyword := range treatmentKeywords {
		if strings.Contains(value, keyword) {
			hasTreatment = true
			break
		}
	}

	if !hasTreatment {
		return passResult(result, "现病史未提及治疗,无需记录疗效。")
	}

	// 检查疗效相关关键词
	effectKeywords := []string{"好转", "缓解", "减轻", "加重", "无效", "有效", "效果", "控制", "稳定", "改善", "恶化"}
	for _, keyword := range effectKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "现病史记录了治疗效果。")
		}
	}

	return failResult(result, "提到治疗后应记录疗效。", "请补充治疗后症状的变化。")
}

// presentIllnessGeneralStatusDiet 一般情况-饮食
func presentIllnessGeneralStatusDiet(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "present_illness")
	result.Evidence["field"] = "present_illness"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "现病史为空。", "请填写现病史。")
	}

	// 检查饮食相关描述
	dietKeywords := []string{"饮食", "食欲", "进食", "纳", "纳可", "纳差"}
	for _, keyword := range dietKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "现病史记录了饮食情况。")
		}
	}

	return failResult(result, "现病史应包含饮食情况。", "请补充患者的饮食或食欲状况。")
}

// presentIllnessGeneralStatusSleep 一般情况-睡眠
func presentIllnessGeneralStatusSleep(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "present_illness")
	result.Evidence["field"] = "present_illness"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "现病史为空。", "请填写现病史。")
	}

	sleepKeywords := []string{"睡眠", "眠", "失眠", "入睡", "多梦"}
	for _, keyword := range sleepKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "现病史记录了睡眠情况。")
		}
	}

	return failResult(result, "现病史应包含睡眠情况。", "请补充患者的睡眠状况。")
}

// presentIllnessGeneralStatusBowel 一般情况-大便
func presentIllnessGeneralStatusBowel(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "present_illness")
	result.Evidence["field"] = "present_illness"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "现病史为空。", "请填写现病史。")
	}

	bowelKeywords := []string{"大便", "便", "排便", "腹泻", "便秘"}
	for _, keyword := range bowelKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "现病史记录了大便情况。")
		}
	}

	return failResult(result, "现病史应包含大便情况。", "请补充患者的大便状况。")
}

// presentIllnessGeneralStatusUrination 一般情况-小便
func presentIllnessGeneralStatusUrination(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "present_illness")
	result.Evidence["field"] = "present_illness"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "现病史为空。", "请填写现病史。")
	}

	urinationKeywords := []string{"小便", "尿", "排尿", "尿频", "尿急", "尿痛"}
	for _, keyword := range urinationKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "现病史记录了小便情况。")
		}
	}

	return failResult(result, "现病史应包含小便情况。", "请补充患者的小便状况。")
}

// presentIllnessGeneralStatusWeightChange 一般情况-体重变化
func presentIllnessGeneralStatusWeightChange(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "present_illness")
	result.Evidence["field"] = "present_illness"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "现病史为空。", "请填写现病史。")
	}

	weightKeywords := []string{"体重", "消瘦", "体重下降", "体重增加", "变瘦", "变胖"}
	for _, keyword := range weightKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "现病史记录了体重变化情况。")
		}
	}

	// 对于体重变化,不强制要求,如果病史较短可以不提
	if len(value) < 50 {
		return passResult(result, "现病史已记录,体重变化可选择性记录。")
	}

	return failResult(result, "现病史应包含体重变化情况。", "请补充患者的体重变化。")
}

// presentIllnessUnaskedNotNone 未询问的内容不应记录为"无"
func presentIllnessUnaskedNotNone(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "present_illness")
	result.Evidence["field"] = "present_illness"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return passResult(result, "现病史为空,规则不适用。")
	}

	// 这是一条需要模型判断的规则,程序无法有效执行
	// 标记为无法判断
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断是否真实询问过"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}

// presentIllnessPhysicalExamConsistency 现病史与体格检查一致
func presentIllnessPhysicalExamConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	present := textValue(data.Content, "present_illness")
	physical := textValue(data.Content, "physical_exam")

	result.Evidence["present_illness"] = present
	result.Evidence["physical_exam"] = physical

	if strings.TrimSpace(present) == "" {
		return failResult(result, "现病史为空,无法比对。", "请填写现病史。")
	}

	if strings.TrimSpace(physical) == "" {
		return failResult(result, "体格检查为空,无法比对。", "请填写体格检查。")
	}

	// 此规则需要深度语义判断,程序难以实现
	result.ExecutionStatus = "incomplete"
	result.IncompleteReason = "此规则需要模型判断现病史阳性症状与体格检查的一致性"
	result.Conclusion = "未判断"
	result.CheckStatus = "未判断"
	result.HandlingResult = "跳过"
	return result
}
