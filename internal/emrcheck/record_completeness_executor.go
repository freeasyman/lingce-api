package emrcheck

import (
	"fmt"
	"strings"
)

// evaluateRecordCompleteness 执行可以仅依据当前结构化病历和就诊上下文
// 完成的正文闭环规则。
//
// 本执行器不连接外部检查、处方或收费系统，也不把模型可能“猜到”的
// 检查项目当成真实对象。只有输入明确提供了外部对象清单时，才进行
// 对象与病历正文之间的对应校验；没有对象清单时，规则不会凭空制造
// 缺项或冲突。这是为了避免调试阶段把“系统没有接入数据”误报成
// “医生没有记录”。
//
// 署名：Codex
// 时间：2026-09-14
func evaluateRecordCompleteness(code string, result evaluatedResult, data checkContext) evaluatedResult {
	result.Evidence["source"] = "record_content/encounter_context"
	switch code {
	case "emr.physical_exam.vital_signs":
		return evaluateVitalSigns(result, data.Content)
	case "emr.auxiliary_exam.item_recorded":
		return evaluateAuxiliaryExam(result, data, "item")
	case "emr.auxiliary_exam.result_recorded":
		return evaluateAuxiliaryExam(result, data, "result")
	case "emr.auxiliary_exam.pending_explicit":
		return evaluateAuxiliaryPending(result, data)
	case "emr.diagnosis.followup_plan":
		return evaluateDiagnosisFollowupPlan(result, data)
	case "emr.treatment.consistency.recorded":
		return evaluateTreatmentRecorded(result, data)
	case "emr.signature.supervisor":
		return evaluateSupervisorSignature(result, data)
	default:
		result.ExecutionStatus = "executor_not_implemented"
		result.Conclusion = "无法完成"
		result.CheckStatus = "未完成"
		result.IncompleteReason = "正文闭环执行器未实现"
		result.HandlingResult = "系统配置缺失"
		result.Explanation = fmt.Sprintf("正文闭环规则未实现：%s。", code)
		return result
	}
}

func evaluateVitalSigns(result evaluatedResult, content map[string]any) evaluatedResult {
	physical := objectValue(content, "physical_exam")
	vitals := objectValue(physical, "vitals")
	if len(vitals) == 0 {
		vitals = objectValue(content, "vital_signs")
	}
	result.Evidence["field"] = "physical_exam.vitals"
	result.Evidence["vitals"] = vitals
	if len(vitals) > 0 && hasMeaningfulValue(vitals) {
		return passResult(result, "已记录至少一项生命体征。")
	}
	for _, key := range []string{"not_measured_reason", "未测原因", "not_taken_reason"} {
		if textValue(physical, key) != "" || textValue(content, key) != "" {
			result.Evidence["not_measured_reason"] = firstNonEmpty(textValue(physical, key), textValue(content, key))
			return passResult(result, "未测生命体征已有明确原因。")
		}
	}
	return failResult(result, "未找到生命体征记录或明确未测原因。", "请补充关键生命体征，或说明未测原因。")
}

func evaluateAuxiliaryExam(result evaluatedResult, data checkContext, mode string) evaluatedResult {
	exam := objectValue(data.Content, "auxiliary_exam")
	text := textValue(data.Content, "auxiliary_exam")
	if text == "" {
		text = firstNonEmpty(textValue(exam, "summary_text"), textValue(exam, "full_text"))
	}
	objects := auxiliaryObjects(data.EncounterContext)
	result.Evidence["field"] = "auxiliary_exam"
	result.Evidence["object_count"] = len(objects)
	result.Evidence["recorded_text_present"] = strings.TrimSpace(text) != ""
	if len(objects) == 0 {
		if strings.TrimSpace(text) != "" {
			return passResult(result, "病历已有辅助检查记录，当前输入未提供外部检查对象清单。")
		}
		return passResult(result, "当前输入没有明确的外部检查对象，暂不判定为缺项。")
	}
	missing := make([]string, 0)
	for _, object := range objects {
		if mode == "item" && !containsAny(text, objectName(object)) {
			missing = append(missing, objectName(object))
		}
		if mode == "result" && auxiliaryObjectHasResult(object) && !containsAny(text, objectResult(object)) {
			missing = append(missing, objectName(object))
		}
	}
	result.Evidence["missing_items"] = missing
	if len(missing) > 0 {
		label := "检查项目"
		if mode == "result" {
			label = "检查结果"
		}
		return failResult(result, label+"没有完整写入病历。", "请补充"+label+"记录。")
	}
	return passResult(result, "已提供的辅助检查对象均能在病历中找到对应记录。")
}

func evaluateAuxiliaryPending(result evaluatedResult, data checkContext) evaluatedResult {
	exam := objectValue(data.Content, "auxiliary_exam")
	text := firstNonEmpty(textValue(data.Content, "auxiliary_exam"), textValue(exam, "summary_text"), textValue(exam, "full_text"))
	objects := auxiliaryObjects(data.EncounterContext)
	pending := false
	for _, object := range objects {
		status := strings.ToLower(strings.TrimSpace(objectString(object, "result_status", "status")))
		if status == "pending" || status == "待回" || status == "ordered" || status == "已开具" {
			pending = true
			break
		}
	}
	result.Evidence["object_count"] = len(objects)
	result.Evidence["pending_object"] = pending
	if !pending {
		return passResult(result, "当前输入没有明确的待回检查对象。")
	}
	if containsAny(text, "待回", "待出结果", "结果未回", "尚未回报", "待报告") {
		return passResult(result, "待回检查已有明确状态说明。")
	}
	return failResult(result, "已开具但未回报的检查没有明确说明。", "请注明检查结果待回或补充检查结果。")
}

func evaluateDiagnosisFollowupPlan(result evaluatedResult, data checkContext) evaluatedResult {
	diagnosis := objectValue(data.Content, "diagnosis")
	status := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		textValue(diagnosis, "status"), textValue(diagnosis, "diagnosis_status"),
		textValue(data.Content, "diagnosis_status"),
	)))
	diagnosisText := firstNonEmpty(textValue(data.Content, "diagnosis"), textValue(diagnosis, "name"))
	pending := status == "待查" || status == "pending" || status == "未明确" ||
		containsAny(diagnosisText, "待查", "待明确", "待排")
	result.Evidence["diagnosis_status"] = status
	result.Evidence["pending_diagnosis"] = pending
	if !pending {
		return passResult(result, "当前诊断没有明确的待查标记。")
	}
	plan := firstNonEmpty(
		textValue(diagnosis, "followup_plan"), textValue(diagnosis, "plan"),
		textValue(data.Content, "followup"), textValue(data.Content, "follow_up"),
		textValue(data.Content, "medical_advice"), textValue(data.Content, "disposition"),
	)
	result.Evidence["plan_present"] = strings.TrimSpace(plan) != ""
	if strings.TrimSpace(plan) == "" {
		return failResult(result, "待查诊断没有进一步检查或处理计划。", "请补充待查诊断的下一步检查或处理计划。")
	}
	return passResult(result, "待查诊断已有进一步检查或处理计划。")
}

func evaluateTreatmentRecorded(result evaluatedResult, data checkContext) evaluatedResult {
	contentFields := map[string]string{
		"prescription":   firstNonEmpty(textValue(data.Content, "prescription"), textValue(data.Content, "drug_prescription")),
		"disposition":    firstNonEmpty(textValue(data.Content, "disposition"), textValue(data.Content, "treatment_orders")),
		"medical_advice": textValue(data.Content, "medical_advice"),
	}
	objects := treatmentObjects(data.EncounterContext)
	result.Evidence["object_count"] = len(objects)
	result.Evidence["recorded_fields"] = contentFields
	if len(objects) == 0 {
		return passResult(result, "当前输入没有明确的外部处方、处置或医嘱对象。")
	}
	missing := make([]string, 0)
	for _, object := range objects {
		kind := strings.ToLower(strings.TrimSpace(objectString(object, "kind", "type", "category")))
		field := "medical_advice"
		if containsAny(kind, "prescription", "drug", "处方", "药") {
			field = "prescription"
		} else if containsAny(kind, "disposition", "treatment", "procedure", "处置", "治疗", "操作") {
			field = "disposition"
		}
		if strings.TrimSpace(contentFields[field]) == "" {
			missing = append(missing, firstNonEmpty(objectString(object, "name", "item_name", "drug_name"), field))
		}
	}
	result.Evidence["missing_records"] = missing
	if len(missing) > 0 {
		return failResult(result, "外部治疗对象在病历正文中没有对应记录。", "请补充对应的处方、处置或医嘱记录。")
	}
	return passResult(result, "外部处方、处置或医嘱对象均有对应正文记录。")
}

func evaluateSupervisorSignature(result evaluatedResult, data checkContext) evaluatedResult {
	required := lookupBool(data.RecordContext, "supervisor_required", "requires_supervisor", "need_supervisor")
	result.Evidence["supervisor_required"] = required
	if !required {
		return passResult(result, "当前病历没有明确要求上级签名。")
	}
	supervisor := firstNonEmpty(
		firstContextValue(data.RecordContext, "supervisor_id", "supervisor_name", "supervisor_confirmed_by"),
		firstContextValue(data.EncounterContext, "supervisor_id", "supervisor_name", "supervisor_confirmed_by"),
	)
	result.Evidence["supervisor"] = supervisor
	if supervisor == "" {
		return failResult(result, "当前病历要求上级签名，但没有找到上级确认记录。", "请完成上级医生确认。")
	}
	return passResult(result, "上级签名要求已有确认记录。")
}

func auxiliaryObjects(context map[string]any) []map[string]any {
	return objectList(context, "auxiliary_exams", "auxiliary_exam_items", "ordered_exams", "exam_orders")
}

func treatmentObjects(context map[string]any) []map[string]any {
	return objectList(context, "prescriptions", "drug_prescriptions", "treatment_orders", "medical_orders")
}

func objectList(context map[string]any, keys ...string) []map[string]any {
	for _, key := range keys {
		value, ok := context[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case []any:
			items := make([]map[string]any, 0, len(typed))
			for _, item := range typed {
				if object, ok := item.(map[string]any); ok {
					items = append(items, object)
				}
			}
			return items
		case []map[string]any:
			return typed
		}
	}
	return nil
}

func objectString(object map[string]any, keys ...string) string {
	return lookupContextValue(object, keys...)
}

func objectName(object map[string]any) string {
	return firstNonEmpty(objectString(object, "item_name", "name", "code"), "未命名检查")
}

func objectResult(object map[string]any) string {
	return firstNonEmpty(objectString(object, "result_text", "result", "conclusion"), objectName(object))
}

func auxiliaryObjectHasResult(object map[string]any) bool {
	status := strings.ToLower(strings.TrimSpace(objectString(object, "result_status", "status")))
	return status != "pending" && status != "待回" && status != "ordered" && status != "已开具"
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(strings.ToLower(value), strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func hasMeaningfulValue(values map[string]any) bool {
	for _, value := range values {
		if value == nil {
			continue
		}
		if object, ok := value.(map[string]any); ok {
			if hasMeaningfulValue(object) {
				return true
			}
			continue
		}
		if strings.TrimSpace(fmt.Sprint(value)) != "" && strings.TrimSpace(fmt.Sprint(value)) != "<nil>" {
			return true
		}
	}
	return false
}

func lookupBool(context map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := context[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			return strings.EqualFold(strings.TrimSpace(typed), "true") ||
				strings.TrimSpace(typed) == "是" ||
				strings.TrimSpace(typed) == "需要"
		}
	}
	return false
}
