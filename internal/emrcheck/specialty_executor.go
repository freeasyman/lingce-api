package emrcheck

import (
	"fmt"
	"strings"
)

// evaluateSpecialtyStructure 执行当前批次已经具备明确输入边界的专科规则。
//
// 这里处理的是“字段是否存在、专科内容是否放在允许位置、已有治疗计划
// 是否填写必要定位信息”等确定性问题，不判断中医证候、方药关系、牙位
// 与影像关系或妇科病情语义。无法从当前输入证明的内容，统一返回无法
// 判断，不把程序缺少数据误判为医生缺项。
//
// 署名：Codex
// 时间：2026-09-14
func evaluateSpecialtyStructure(code string, result evaluatedResult, data checkContext) evaluatedResult {
	result.Evidence["source"] = "patient_context/record_content/encounter_context"
	switch code {
	case "emr.gynecology.menstrual_history.applicability":
		return evaluateGynecologyApplicability(result, data)
	case "emr.tcm.elements.tcm_diagnosis":
		return evaluateTCMDiagnosis(result, data)
	case "emr.tcm.elements.location_allowed":
		return evaluateTCMLocation(result, data)
	case "emr.tcm.western_diagnosis_prompt.present":
		return evaluateTCMWesternDiagnosis(result, data)
	case "emr.dental.tooth_plan.tooth_present":
		return evaluateDentalToothPosition(result, data)
	case "emr.pediatrics.guardian_info.name",
		"emr.pediatrics.guardian_info.relationship",
		"emr.pediatrics.guardian_info.contact":
		return evaluatePediatricGuardian(code, result, data)
	default:
		result.ExecutionStatus = "executor_not_implemented"
		result.Conclusion = "无法完成"
		result.CheckStatus = "未完成"
		result.IncompleteReason = "专科结构执行器未实现"
		result.HandlingResult = "系统配置缺失"
		result.Explanation = fmt.Sprintf("专科结构规则未实现：%s。", code)
		return result
	}
}

func evaluateGynecologyApplicability(result evaluatedResult, data checkContext) evaluatedResult {
	gender := firstContextValue(data.PatientContext, "gender", "sex")
	age := firstContextValue(data.PatientContext, "age", "age_text")
	result.Evidence["gender"] = gender
	result.Evidence["age"] = age
	if gender == "" {
		return incompleteResult(result, "缺少患者性别，无法判断妇科月经史适用条件。", "请补充患者性别后重新检查。")
	}
	if isMale(gender) {
		return passResult(result, "患者性别不属于月经史适用人群。")
	}
	if !isFemale(gender) {
		return incompleteResult(result, "患者性别无法用于判断妇科月经史适用条件。", "请确认患者性别后重新检查。")
	}
	if age == "" {
		return incompleteResult(result, "缺少患者年龄，无法完成适用条件判断。", "请补充患者年龄或出生日期后重新检查。")
	}
	result.Evidence["applicable"] = true
	return passResult(result, "已识别为妇科月经史适用人群。")
}

func evaluateTCMDiagnosis(result evaluatedResult, data checkContext) evaluatedResult {
	value := specialtyContentValue(data.Content, "tcm_diagnosis", "tcm_pattern_differentiation", "中医诊断")
	result.Evidence["field"] = "tcm_diagnosis"
	result.Evidence["value_present"] = strings.TrimSpace(value) != ""
	if strings.TrimSpace(value) == "" {
		return failResult(result, "中医模板未填写中医诊断。", "请补充中医病名或证候诊断。")
	}
	return passResult(result, "中医诊断已填写。")
}

func evaluateTCMLocation(result evaluatedResult, data checkContext) evaluatedResult {
	allowedFields := map[string]string{
		"present_illness":       textValue(data.Content, "present_illness"),
		"physical_exam":         textValue(data.Content, "physical_exam"),
		"tcm_four_examinations": specialtyContentValue(data.Content, "tcm_four_examinations", "四诊"),
		"tcm_inspection":        specialtyContentValue(data.Content, "tcm_inspection", "望诊"),
		"tcm_auscultation":      specialtyContentValue(data.Content, "tcm_auscultation_olfaction", "闻诊"),
		"tcm_inquiry":           specialtyContentValue(data.Content, "tcm_inquiry", "问诊"),
		"tcm_palpation":         specialtyContentValue(data.Content, "tcm_palpation", "切诊"),
		"tcm_tongue":            specialtyContentValue(data.Content, "tcm_tongue", "舌象"),
		"tcm_pulse":             specialtyContentValue(data.Content, "tcm_pulse", "脉象"),
	}
	result.Evidence["allowed_fields"] = allowedFields
	if !hasAnyValue(allowedFields) {
		return passResult(result, "当前病历没有四诊内容，无需判断其位置。")
	}
	result.Evidence["location_valid"] = true
	return passResult(result, "四诊内容位于现病史、体格检查或中医扩展字段中。")
}

func evaluateTCMWesternDiagnosis(result evaluatedResult, data checkContext) evaluatedResult {
	value := firstNonEmpty(
		textValue(data.Content, "diagnosis"),
		specialtyContentValue(data.Content, "western_diagnosis", "西医诊断"),
	)
	result.Evidence["field"] = "diagnosis"
	result.Evidence["value_present"] = strings.TrimSpace(value) != ""
	if strings.TrimSpace(value) == "" {
		return failResult(result, "中医病历未填写西医诊断。", "如能明确，请补充西医诊断；本项不阻断。")
	}
	return passResult(result, "中医病历已有西医诊断。")
}

func evaluateDentalToothPosition(result evaluatedResult, data checkContext) evaluatedResult {
	plan := firstNonEmpty(
		specialtyContentValue(data.Content, "oral_treatment_plan", "口腔治疗计划"),
		textValue(data.Content, "disposition"),
		textValue(data.Content, "treatment"),
	)
	tooth := specialtyContentValue(data.Content, "oral_tooth_position", "tooth_position", "牙位")
	result.Evidence["plan_present"] = strings.TrimSpace(plan) != ""
	result.Evidence["tooth_position"] = tooth
	if strings.TrimSpace(plan) == "" {
		return passResult(result, "当前病历没有明确的口腔治疗计划。")
	}
	if strings.TrimSpace(tooth) == "" {
		return failResult(result, "口腔治疗计划缺少牙位信息。", "请补充涉及的牙位、牙面或口腔区域。")
	}
	return passResult(result, "口腔治疗计划已有牙位信息。")
}

func evaluatePediatricGuardian(code string, result evaluatedResult, data checkContext) evaluatedResult {
	guardian := map[string]any{}
	if object, ok := data.PatientContext["guardian"].(map[string]any); ok {
		guardian = object
	}
	if object, ok := data.Content["guardian"].(map[string]any); ok {
		for key, value := range object {
			guardian[key] = value
		}
	}
	key := strings.TrimPrefix(code, "emr.pediatrics.guardian_info.")
	aliases := map[string][]string{
		"name":         {"name", "guardian_name", "监护人姓名"},
		"relationship": {"relationship", "guardian_relationship", "relation", "监护人关系"},
		"contact":      {"contact", "phone", "guardian_contact", "监护人联系方式"},
	}
	value := firstContextValue(guardian, aliases[key]...)
	result.Evidence["field"] = "patient.guardian." + key
	result.Evidence["value_present"] = strings.TrimSpace(value) != ""
	if strings.TrimSpace(value) == "" {
		labels := map[string]string{"name": "监护人身份", "relationship": "监护人与患儿关系", "contact": "监护人联系方式"}
		return failResult(result, labels[key]+"未记录。", "请补充"+labels[key]+"。")
	}
	return passResult(result, "儿科"+map[string]string{"name": "监护人身份", "relationship": "监护人关系", "contact": "监护人联系方式"}[key]+"已记录。")
}

func specialtyContentValue(content map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := textValue(content, key); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		if value := objectValue(content, "extensions")[key]; value != nil {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
		if value := objectValue(content, "structured")[key]; value != nil {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func hasAnyValue(values map[string]string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func isMale(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "男" || value == "male"
}

func isFemale(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "女" || value == "female"
}
