package emrcheck

import (
	"fmt"
	"strings"
)

var allowedGenderValues = map[string]struct{}{
	"男": {}, "女": {}, "其他": {}, "未知": {},
	"male": {}, "female": {}, "other": {}, "unknown": {},
}

// evaluateBasicInfo 执行患者身份和就诊归属类的确定性规则。
//
// 这些信息不是从病历正文推断，而是从患者快照、就诊上下文和病历记录
// 上下文读取。字段缺失时直接判定为不符合；不会因为病历正文里提到
// 某个姓名、医生或科室，就把患者主数据错误地当成已完成。
//
// 署名：Codex
// 时间：2026-09-14
func evaluateBasicInfo(code string, result evaluatedResult, data checkContext) evaluatedResult {
	result.Evidence["source"] = "patient_context/encounter_context/record_context"
	switch code {
	case "emr.patient.basic_info.name":
		return basicValueResult(result, "patient.name", data.PatientContext, "患者姓名", true)
	case "emr.patient.basic_info.gender":
		value := lookupContextValue(data.PatientContext, "gender", "sex")
		result.Evidence["field"] = "patient.gender"
		result.Evidence["value"] = value
		if value == "" {
			return failResult(result, "患者性别为空。", "请补充患者性别。")
		}
		if _, ok := allowedGenderValues[strings.ToLower(value)]; !ok {
			return failResult(result, "患者性别不在允许值范围内。", "请修正患者性别。")
		}
		return passResult(result, "患者性别已填写且值有效。")
	case "emr.patient.basic_info.birth_or_age":
		birthDate := lookupContextValue(data.PatientContext, "birth_date", "date_of_birth")
		age := lookupContextValue(data.PatientContext, "age", "age_text")
		result.Evidence["fields"] = []string{"patient.birth_date", "patient.age", "patient.age_text"}
		result.Evidence["birth_date"] = birthDate
		result.Evidence["age"] = age
		if birthDate == "" && age == "" {
			return failResult(result, "患者出生日期和年龄均为空。", "请补充患者出生日期或年龄。")
		}
		if birthDate != "" && !isDateLike(birthDate) {
			return failResult(result, "患者出生日期无法解析。", "请修正患者出生日期。")
		}
		if age != "" && !hasNumericContent(age) {
			return failResult(result, "患者年龄无法解析。", "请修正患者年龄。")
		}
		return passResult(result, "患者出生日期或年龄已填写且可解析。")
	case "emr.patient.basic_info.phone":
		return basicValueResult(result, "patient.phone", data.PatientContext, "患者联系电话", true)
	case "emr.patient.basic_info.address":
		return basicValueResult(result, "patient.address", data.PatientContext, "患者联系地址", true)
	case "emr.patient.basic_info.ethnicity":
		return basicValueResult(result, "patient.ethnicity", data.PatientContext, "患者民族", true)
	case "emr.patient.basic_info.marital_status":
		return basicValueResult(result, "patient.marital_status", data.PatientContext, "婚姻状况", true)
	case "emr.patient.basic_info.occupation":
		return basicValueResult(result, "patient.occupation", data.PatientContext, "患者职业", true)
	case "emr.patient.basic_info.encounter_time":
		value := firstContextValue(data.RecordContext, "encountered_at", "encounter_time", "started_at")
		if value == "" {
			value = firstContextValue(data.EncounterContext, "encountered_at", "encounter_time", "recorded_at")
		}
		result.Evidence["field"] = "record.encountered_at"
		result.Evidence["value"] = value
		if value == "" || !isDateTimeLike(value) {
			return failResult(result, "就诊时间为空或无法解析。", "请补充或修正就诊时间。")
		}
		return passResult(result, "就诊时间已填写且可解析。")
	case "emr.patient.basic_info.department":
		value := firstContextValue(data.RecordContext, "department_id", "department_code", "medical_department_code")
		if value == "" {
			value = firstContextValue(data.EncounterContext, "department_id", "department_code", "medical_department_code")
		}
		result.Evidence["field"] = "record.department"
		result.Evidence["value"] = value
		if value == "" {
			return failResult(result, "接诊科室为空。", "请补充接诊科室。")
		}
		return passResult(result, "接诊科室已填写。")
	case "emr.patient.basic_info.attending_doctor":
		value := firstContextValue(data.RecordContext, "doctor_id", "attending_doctor", "doctor_name")
		if value == "" {
			value = firstContextValue(data.EncounterContext, "doctor_id", "employee_id", "doctor_name")
		}
		result.Evidence["field"] = "record.doctor_id"
		result.Evidence["value"] = value
		if value == "" {
			return failResult(result, "接诊医生为空。", "请补充接诊医生。")
		}
		return passResult(result, "接诊医生已填写。")
	case "emr.patient.basic_info.pending_confirmation":
		pending := lookupPendingConfirmation(data.PatientContext, data.EncounterContext, data.RecordContext)
		result.Evidence["pending_confirmation"] = pending
		if pending {
			return failResult(result, "患者基本信息仍待确认。", "请完成患者基本信息确认后再归档。")
		}
		return passResult(result, "患者基本信息没有待确认标记。")
	default:
		result.ExecutionStatus = "executor_not_implemented"
		result.Conclusion = "无法完成"
		result.CheckStatus = "未完成"
		result.IncompleteReason = "基础信息执行器未实现"
		result.HandlingResult = "系统配置缺失"
		result.Explanation = fmt.Sprintf("基础信息规则未实现：%s。", code)
		return result
	}
}

func basicValueResult(result evaluatedResult, field string, context map[string]any, label string, required bool) evaluatedResult {
	value := lookupContextValue(context, strings.TrimPrefix(field, "patient."))
	result.Evidence["field"] = field
	result.Evidence["value"] = value
	if required && value == "" {
		return failResult(result, label+"为空。", "请补充"+label+"。")
	}
	return passResult(result, label+"已填写。")
}

func lookupContextValue(context map[string]any, keys ...string) string {
	for _, key := range keys {
		parts := strings.Split(key, ".")
		var current any = context
		for _, part := range parts {
			object, ok := current.(map[string]any)
			if !ok {
				current = nil
				break
			}
			current, ok = object[part]
			if !ok {
				current = nil
				break
			}
		}
		if current == nil {
			continue
		}
		value := strings.TrimSpace(fmt.Sprint(current))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func firstContextValue(context map[string]any, keys ...string) string {
	return lookupContextValue(context, keys...)
}

func lookupPendingConfirmation(contexts ...map[string]any) bool {
	for _, context := range contexts {
		for _, key := range []string{"pending_confirmation", "patient_pending_confirmation", "confirmation_status"} {
			value, ok := context[key]
			if !ok {
				continue
			}
			switch typed := value.(type) {
			case bool:
				if typed {
					return true
				}
			case string:
				normalized := strings.TrimSpace(strings.ToLower(typed))
				if normalized == "true" || normalized == "待确认" || normalized == "pending" || normalized == "未确认" {
					return true
				}
			}
		}
		if values, ok := context["pending_fields"].([]any); ok && len(values) > 0 {
			return true
		}
	}
	return false
}

func hasNumericContent(value string) bool {
	for _, character := range value {
		if character >= '0' && character <= '9' {
			return true
		}
	}
	return false
}

func isDateLike(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 8 {
		return false
	}
	return strings.Contains(value, "-") || strings.Contains(value, "/") || hasNumericContent(value)
}

func isDateTimeLike(value string) bool {
	return isDateLike(value) && (strings.Contains(value, "T") || strings.Contains(value, " ") || len(value) >= 10)
}
