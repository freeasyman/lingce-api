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
	case "emr.pediatrics.fever_peak.temperature":
		return evaluatePediatricFever(result, data, "temperature")
	case "emr.pediatrics.fever_peak.duration":
		return evaluatePediatricFever(result, data, "duration")
	case "emr.pediatrics.weight_dose.age":
		return evaluatePediatricPrescription(result, data, "age")
	case "emr.pediatrics.weight_dose.weight_basis":
		return evaluatePediatricPrescription(result, data, "weight_basis")
	case "emr.pediatrics.weight_dose.dose":
		return evaluatePediatricPrescription(result, data, "dose")
	case "emr.pediatrics.weight_dose.route":
		return evaluatePediatricPrescription(result, data, "route")
	case "emr.pediatrics.weight_dose.course":
		return evaluatePediatricPrescription(result, data, "course")
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

// evaluatePediatricFever 只在病历明确出现发热场景时检查发热病史。
// 没有发热主诉时直接视为不适用，避免把所有儿科病历都误报为发热病历。
// 本函数只判断字段是否有明确记录，不推断体温是否正常。
//
// 署名：Codex
// 时间：2026-09-14
func evaluatePediatricFever(result evaluatedResult, data checkContext, field string) evaluatedResult {
	chiefComplaint := textValue(data.Content, "chief_complaint")
	presentIllness := textValue(data.Content, "present_illness")
	temperature := pediatricTemperature(data)
	duration := pediatricDuration(data)
	hasFever := containsAny(chiefComplaint, "发热", "发烧", "高热", "低热", "fever") ||
		containsAny(presentIllness, "发热", "发烧", "高热", "低热", "fever") ||
		meaningfulText(temperature) || meaningfulText(duration)

	result.Evidence["trigger_fields"] = map[string]any{
		"chief_complaint": chiefComplaint,
		"present_illness": presentIllness,
		"fever_detected":  hasFever,
	}
	result.Evidence["field"] = "structured." + field
	if field == "temperature" {
		result.Evidence["temperature"] = temperature
	} else {
		result.Evidence["duration"] = duration
	}
	if !hasFever {
		result.Evidence["not_applicable_reason"] = "当前病历没有明确发热主诉或发热记录"
		return passResult(result, "当前病历没有明确发热场景，无需填写儿童发热峰值信息。")
	}
	if field == "temperature" && !meaningfulText(temperature) {
		return failResult(result, "发热病史未记录最高体温。", "请补充最高体温或明确说明无法测量的原因。")
	}
	if field == "duration" && !meaningfulText(duration) {
		return failResult(result, "发热病史未记录持续时间。", "请补充发热持续时间。")
	}
	return passResult(result, map[string]string{
		"temperature": "发热病史已记录最高体温。",
		"duration":    "发热病史已记录持续时间。",
	}[field])
}

// evaluatePediatricPrescription 检查儿科处方是否具备当前结构化输入
// 能直接确认的基础信息。它不连接药品知识库，也不计算剂量是否安全；
// 这些工作仍由后续知识库/人工复核规则负责。
//
// 署名：Codex
// 时间：2026-09-14
func evaluatePediatricPrescription(result evaluatedResult, data checkContext, field string) evaluatedResult {
	prescription := pediatricPrescription(data)
	result.Evidence["prescription_present"] = prescription.Present
	result.Evidence["prescription_text"] = prescription.Text
	result.Evidence["field"] = "prescription." + field
	if !prescription.Present {
		result.Evidence["not_applicable_reason"] = "当前输入没有明确儿科处方"
		return passResult(result, "当前病历没有明确儿科处方，无需检查处方剂量信息。")
	}

	value := ""
	switch field {
	case "age":
		value = firstContextValue(data.PatientContext, "age", "age_text", "birth_date", "date_of_birth")
	case "weight_basis":
		value = firstNonEmpty(
			firstContextValue(data.PatientContext, "weight", "weight_kg", "body_weight"),
			prescription.Value("weight_basis", "dose_basis", "weight"),
		)
	case "dose":
		value = prescription.Value("dose", "single_dose", "dose_amount", "dosage")
		if value == "" && hasDoseText(prescription.Text) {
			value = prescription.Text
		}
	case "route":
		value = prescription.Value("route", "administration_route", "usage", "frequency")
		if value == "" && hasRouteText(prescription.Text) {
			value = prescription.Text
		}
	case "course":
		value = prescription.Value("course", "duration", "treatment_course", "days")
		if value == "" && hasCourseText(prescription.Text) {
			value = prescription.Text
		}
	}
	result.Evidence["value_present"] = meaningfulText(value)
	if !meaningfulText(value) {
		labels := map[string]string{
			"age":          "患者年龄依据",
			"weight_basis": "体重或明确剂量依据",
			"dose":         "药品剂量",
			"route":        "用法或给药途径",
			"course":       "疗程",
		}
		return failResult(result, "儿科处方缺少"+labels[field]+"。", "请补充"+labels[field]+"。")
	}
	return passResult(result, "儿科处方已记录"+map[string]string{
		"age":          "年龄依据",
		"weight_basis": "体重或剂量依据",
		"dose":         "药品剂量",
		"route":        "用法或给药途径",
		"course":       "疗程",
	}[field]+"。")
}

type pediatricPrescriptionInfo struct {
	Present bool
	Text    string
	Objects []map[string]any
}

func pediatricPrescription(data checkContext) pediatricPrescriptionInfo {
	objects := treatmentObjects(data.EncounterContext)
	prescriptionObjects := make([]map[string]any, 0, len(objects))
	for _, object := range objects {
		kind := strings.ToLower(objectString(object, "kind", "type", "category"))
		if containsAny(kind, "prescription", "drug", "处方", "药") {
			prescriptionObjects = append(prescriptionObjects, object)
		}
	}
	text := firstNonEmpty(textValue(data.Content, "prescription"), textValue(data.Content, "drug_prescription"))
	return pediatricPrescriptionInfo{
		Present: len(prescriptionObjects) > 0 || meaningfulText(text),
		Text:    text,
		Objects: prescriptionObjects,
	}
}

func (p pediatricPrescriptionInfo) Value(keys ...string) string {
	for _, object := range p.Objects {
		if value := objectString(object, keys...); meaningfulText(value) {
			return value
		}
	}
	return ""
}

func pediatricTemperature(data checkContext) string {
	physical := objectValue(data.Content, "physical_exam")
	vitals := objectValue(physical, "vitals")
	return firstNonEmpty(
		specialtyContentValue(data.Content, "temperature", "temperature_c", "max_temperature", "highest_temperature", "fever_peak_temperature"),
		firstContextValue(vitals, "temperature", "temperature_c", "temp"),
		firstContextValue(data.PatientContext, "temperature", "temperature_c", "max_temperature"),
	)
}

func pediatricDuration(data checkContext) string {
	return firstNonEmpty(
		specialtyContentValue(data.Content, "fever_duration", "fever_days", "duration_of_fever"),
		textValue(data.Content, "fever_history"),
	)
}

func meaningfulText(value string) bool {
	normalized := strings.TrimSpace(strings.ToLower(value))
	return normalized != "" && normalized != "<nil>" && normalized != "无" && normalized != "否认" && normalized != "none"
}

func hasDoseText(value string) bool {
	return containsAny(value, "mg", "毫克", "克", "ml", "毫升", "片", "粒", "喷", "滴", "每次")
}

func hasRouteText(value string) bool {
	return containsAny(value, "口服", "外用", "静脉", "肌注", "皮下", "吸入", "滴眼", "用法", "每日", "一天")
}

func hasCourseText(value string) bool {
	return containsAny(value, "疗程", "天", "周", "月", "连续", "共")
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
