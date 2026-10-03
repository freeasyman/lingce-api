package emrcheck

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ========== 主诉 chief_complaint 执行器 ==========

// chiefComplaintSymptomSign 主诉包含症状或体征
func chiefComplaintSymptomSign(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "chief_complaint")
	result.Evidence["field"] = "chief_complaint"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "主诉为空,无法判断是否包含症状或体征。", "请填写主诉。")
	}

	// 简单启发式:包含常见症状词汇
	symptomKeywords := []string{"痛", "疼", "胀", "痒", "热", "咳", "喘", "吐", "泻", "晕", "肿", "麻", "酸", "困", "乏", "憋"}
	for _, keyword := range symptomKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "主诉包含症状或体征描述。")
		}
	}

	return failResult(result, "主诉应包含具体的症状或体征。", "请用症状性语言描述患者的主要问题。")
}

// chiefComplaintNotVague 主诉不应过于笼统
func chiefComplaintNotVague(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "chief_complaint")
	result.Evidence["field"] = "chief_complaint"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "主诉为空。", "请填写主诉。")
	}

	// 检查是否是笼统描述
	vagueTerms := []string{"不舒服", "不适", "复诊", "检查", "咨询", "配药", "拿药"}
	for _, term := range vagueTerms {
		if strings.Contains(value, term) && utf8.RuneCountInString(value) < 10 {
			return failResult(result,
				fmt.Sprintf("主诉“%s”过于笼统。", value),
				"请描述具体的症状或体征。")
		}
	}

	return passResult(result, "主诉描述具体。")
}

// chiefComplaintDuration 主诉包含病程或时长
func chiefComplaintDuration(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "chief_complaint")
	result.Evidence["field"] = "chief_complaint"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "主诉为空。", "请填写主诉。")
	}

	// 检查是否包含时长表达
	durationKeywords := []string{"天", "日", "周", "月", "年", "小时", "分钟", "余", "许"}
	for _, keyword := range durationKeywords {
		if strings.Contains(value, keyword) {
			return passResult(result, "主诉包含病程时长信息。")
		}
	}

	return failResult(result, "主诉未包含病程时长。", "请在主诉中说明症状持续时间。")
}

// chiefComplaintNotDiagnosisOnly 主诉不应只有诊断名称
func chiefComplaintNotDiagnosisOnly(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	value := textValue(data.Content, "chief_complaint")
	result.Evidence["field"] = "chief_complaint"
	result.Evidence["value"] = value

	if strings.TrimSpace(value) == "" {
		return failResult(result, "主诉为空。", "请填写主诉。")
	}

	// 检查是否只包含诊断名词而没有症状描述
	diagnosisOnlyTerms := []string{"高血压", "糖尿病", "冠心病", "肺炎", "胃炎", "肠炎"}
	length := utf8.RuneCountInString(value)

	for _, term := range diagnosisOnlyTerms {
		if value == term || (strings.Contains(value, term) && length < len(term)+3) {
			return failResult(result,
				"主诉不应仅为诊断名称。",
				"请描述患者的具体症状或主诉原因。")
		}
	}

	return passResult(result, "主诉描述恰当。")
}

// chiefComplaintPresentIllnessConsistency 主诉与现病史一致
func chiefComplaintPresentIllnessConsistency(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)
	chief := textValue(data.Content, "chief_complaint")
	present := textValue(data.Content, "present_illness")

	result.Evidence["chief_complaint"] = chief
	result.Evidence["present_illness"] = present

	if strings.TrimSpace(chief) == "" {
		return failResult(result, "主诉为空,无法比对。", "请填写主诉。")
	}

	if strings.TrimSpace(present) == "" {
		return failResult(result, "现病史为空,无法比对。", "请填写现病史。")
	}

	// 简单启发式:现病史应该包含主诉中的关键词
	// 提取主诉中的关键词(去除时长后缀)
	chiefKeywords := extractKeySymptoms(chief)
	if len(chiefKeywords) == 0 {
		return passResult(result, "主诉与现病史已记录。")
	}

	// 检查现病史是否包含主诉关键词
	for _, keyword := range chiefKeywords {
		if strings.Contains(present, keyword) {
			return passResult(result, "主诉与现病史描述一致。")
		}
	}

	return failResult(result,
		"现病史未展开主诉内容。",
		"请在现病史中详细描述主诉症状的发生发展过程。")
}

// extractKeySymptoms 从主诉中提取关键症状词
func extractKeySymptoms(chief string) []string {
	// 去除常见的时长后缀
	cleanChief := chief
	timeSuffixes := []string{"天", "日", "周", "月", "年", "小时", "分钟", "余"}
	for _, suffix := range timeSuffixes {
		if idx := strings.LastIndex(cleanChief, suffix); idx != -1 {
			cleanChief = cleanChief[:idx+len(suffix)]
		}
	}

	// 提取症状关键词
	symptoms := []string{}
	symptomKeywords := []string{"痛", "疼", "胀", "痒", "热", "咳", "喘", "吐", "泻", "晕", "肿", "麻", "酸", "困", "乏", "憋", "出血", "发热", "腹泻"}
	for _, keyword := range symptomKeywords {
		if strings.Contains(cleanChief, keyword) {
			symptoms = append(symptoms, keyword)
		}
	}
	return symptoms
}
