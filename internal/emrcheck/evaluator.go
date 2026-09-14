package emrcheck

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type binding struct {
	QualityRequirementID string
	Code                 string
	Name                 string
	RuleType             string
	ExecutionMode        string
	DeadlineAction       string
	QualityGroup         string
	EvaluatedFact        string
	PassCondition        string
	Precondition         string
	EvidenceBasis        string
	SourceVersion        string
}

type qualityRuleInput struct {
	QualityRequirementID string `json:"quality_requirement_id"`
	Code                 string `json:"rule_code"`
	Name                 string `json:"rule_name"`
	RuleType             string `json:"rule_type"`
	QualityGroup         string `json:"quality_group"`
	EvaluatedFact        string `json:"evaluated_fact"`
	PassCondition        string `json:"pass_condition"`
	Precondition         string `json:"precondition"`
	EvidenceBasis        string `json:"evidence_basis"`
	SourceVersion        string `json:"source_version"`
	ExecutionMode        string `json:"execution_mode"`
	DeadlineAction       string `json:"deadline_action"`
}

type checkContext struct {
	RecordID          string
	SnapshotID        string
	TemplateVersionID string
	VisitType         string
	Specialty         string
	Content           map[string]any
	PatientContext    map[string]any
	EncounterContext  map[string]any
	RecordContext     map[string]any
	Status            string
	CreatedAt         *time.Time
	StartedAt         *time.Time
	EndedAt           *time.Time
	SubmittedAt       *time.Time
	ArchivedAt        *time.Time
	LastSavedAt       *time.Time
	UpdatedAt         *time.Time
	ConfirmedBy       *int64
}

type evaluatedResult struct {
	Conclusion       string
	CheckStatus      string
	ExecutionStatus  string
	IncompleteReason string
	HandlingResult   string
	MeetsDeadline    *bool
	Evidence         map[string]any
	Explanation      string
	Suggested        string
	ActualMode       string
}

func evaluateRequirement(item binding, data checkContext, trigger string) evaluatedResult {
	result := evaluatedResult{
		Conclusion:      "无法判断",
		CheckStatus:     "未完成",
		ExecutionStatus: "manual_required",
		HandlingResult:  "待人工判断",
		Evidence:        map[string]any{"snapshot_id": data.SnapshotID},
		ActualMode:      item.ExecutionMode,
	}

	if notApplicable(item.Code, data) {
		result.Conclusion = "符合"
		result.CheckStatus = "已完成"
		result.ExecutionStatus = "completed"
		result.HandlingResult = "允许继续"
		result.MeetsDeadline = boolPtr(true)
		result.Evidence["前提"] = "当前病历不具备该要求的事实前提"
		result.Explanation = "当前病历不适用此项要求。"
		return result
	}

	if item.ExecutionMode != "程序判断" {
		result.ActualMode = item.ExecutionMode
		if item.ExecutionMode == "人工判断" {
			result.CheckStatus = "已完成"
			result.ExecutionStatus = "manual_required"
			result.IncompleteReason = ""
			result.HandlingResult = "需要人工复核"
			result.Explanation = "该质量规则配置为人工判断，需要医生或质控人员复核。"
			result.Suggested = "请医生或质控人员完成人工复核。"
			result.MeetsDeadline = boolPtr(false)
			return result
		}
		result.ExecutionStatus = "execution_failed"
		result.IncompleteReason = "人工待处理"
		result.Explanation = fmt.Sprintf("当前配置为%s，当前版本未调用该执行器。", item.ExecutionMode)
		result.Suggested = "转人工判断或在执行器接入后重新检查。"
		result.MeetsDeadline = boolPtr(false)
		result.HandlingResult = handlingForIncomplete(item.DeadlineAction, trigger)
		if result.HandlingResult == "阻断" {
			result.Suggested = "请完成判断后再继续当前动作。"
		}
		return result
	}

	result = evaluateProgram(item, data)
	if result.ActualMode == "" {
		result.ActualMode = "程序判断"
	}
	if result.CheckStatus == "已完成" {
		if result.ActualMode == "人工判断" {
			result.MeetsDeadline = boolPtr(false)
			result.HandlingResult = "需要人工复核"
		} else {
			result.MeetsDeadline = boolPtr(true)
			if result.Conclusion == "不符合" {
				result.MeetsDeadline = boolPtr(false)
				result.HandlingResult = handlingForNonconforming(item.DeadlineAction, trigger)
			} else {
				result.HandlingResult = "允许继续"
			}
		}
	} else {
		result.MeetsDeadline = boolPtr(false)
		if result.ExecutionStatus == "completed" && (result.HandlingResult == "" || result.HandlingResult == "待人工判断") {
			result.HandlingResult = handlingForIncomplete(item.DeadlineAction, trigger)
		}
	}
	return result
}

func evaluateProgram(item binding, data checkContext) evaluatedResult {
	result := evaluatedResult{
		Conclusion:      "无法判断",
		CheckStatus:     "未完成",
		ExecutionStatus: "completed",
		HandlingResult:  "待人工判断",
		Evidence:        map[string]any{"snapshot_id": data.SnapshotID},
	}

	switch item.Code {
	case "emr.patient.basic_info.name", "emr.patient.basic_info.gender",
		"emr.patient.basic_info.birth_or_age", "emr.patient.basic_info.phone",
		"emr.patient.basic_info.address", "emr.patient.basic_info.ethnicity",
		"emr.patient.basic_info.marital_status", "emr.patient.basic_info.occupation",
		"emr.patient.basic_info.encounter_time", "emr.patient.basic_info.department",
		"emr.patient.basic_info.attending_doctor", "emr.patient.basic_info.pending_confirmation":
		return evaluateBasicInfo(item.Code, result, data)
	case "emr.time.consistency.fields_present", "emr.time.consistency.order",
		"emr.time.consistency.last_modified", "emr.time.consistency.abnormal_span":
		return evaluateTimeConsistency(item.Code, result, data)
	case "emr.physical_exam.vital_signs", "emr.auxiliary_exam.item_recorded",
		"emr.auxiliary_exam.result_recorded", "emr.auxiliary_exam.pending_explicit",
		"emr.diagnosis.followup_plan", "emr.treatment.consistency.recorded",
		"emr.signature.supervisor":
		return evaluateRecordCompleteness(item.Code, result, data)
	case "emr.chief_complaint.required":
		return requiredResult(result, data.Content, "chief_complaint", "主诉")
	case "emr.chief_complaint.max_length":
		value := textValue(data.Content, "chief_complaint")
		result.Evidence["field"] = "chief_complaint"
		result.Evidence["length"] = utf8.RuneCountInString(value)
		if strings.TrimSpace(value) == "" {
			return failResult(result, "主诉为空，无法满足字数要求。", "请先填写主诉。")
		}
		if utf8.RuneCountInString(value) <= 20 {
			return passResult(result, "主诉不超过20个字符。")
		}
		return failResult(result, "主诉超过20个字符。", "请压缩主诉内容。")
	case "emr.allergy.required.status":
		return allergyStatusResult(result, data.Content)
	case "emr.allergy.prescription_risk.known_allergy":
		return allergyPrescriptionRiskResult(result, data)
	case "emr.allergy.required.unconfirmed":
		return allergyConfirmedResult(result, data.Content)
	case "emr.allergy.required.drug_detail":
		return allergyDetailResult(result, data.Content, "具体药物过敏", "drug_name", "请补充具体的过敏药物名称。")
	case "emr.allergy.required.other_detail":
		return allergyDetailResult(result, data.Content, "其他过敏原", "other_allergen", "请补充其他过敏原说明。")
	case "emr.gynecology.menstrual_history.applicability",
		"emr.tcm.elements.tcm_diagnosis",
		"emr.tcm.elements.location_allowed",
		"emr.tcm.western_diagnosis_prompt.present",
		"emr.dental.tooth_plan.tooth_present",
		"emr.pediatrics.guardian_info.name",
		"emr.pediatrics.guardian_info.relationship",
		"emr.pediatrics.guardian_info.contact",
		"emr.pediatrics.fever_peak.temperature",
		"emr.pediatrics.fever_peak.duration",
		"emr.pediatrics.weight_dose.age",
		"emr.pediatrics.weight_dose.weight_basis",
		"emr.pediatrics.weight_dose.dose",
		"emr.pediatrics.weight_dose.route",
		"emr.pediatrics.weight_dose.course":
		return evaluateSpecialtyStructure(item.Code, result, data)
	case "emr.diagnosis.present":
		return requiredResult(result, data.Content, "diagnosis", "诊断")
	case "emr.signature.doctor":
		result.Evidence["confirmed_by"] = data.ConfirmedBy
		if data.ConfirmedBy != nil && *data.ConfirmedBy > 0 {
			return passResult(result, "病历已有有效责任确认。")
		}
		return failResult(result, "尚未找到责任医生确认记录。", "请由责任医生确认病历。")
	default:
		// 署名：Codex；时间：2026-09-14。
		// 规则表中标记为“程序判断”只代表这条规则理论上可以由确定性
		// 程序执行；如果当前执行器尚未实现对应 rule_code，不能继续
		// 伪装成“程序判断未完成”，否则页面会误解为执行中的程序问题，
		// 总体结论也可能被错误压成“无问题”。这里明确转为人工复核，
		// 让调试日志和前端都能看到真实原因。
		result.ActualMode = "程序判断"
		result.Conclusion = "无法完成"
		result.CheckStatus = "未完成"
		result.ExecutionStatus = "executor_not_implemented"
		result.IncompleteReason = "程序执行器未实现"
		result.HandlingResult = "系统配置缺失"
		result.Explanation = "当前版本没有为这条质量要求配置确定性程序判断执行器。"
		result.Suggested = "请补充该规则的程序执行器后重新检查。"
		result.Evidence["rule_code"] = item.Code
		result.Evidence["reason"] = "program_executor_not_implemented"
		return result
	}
}

func allergyStatusResult(result evaluatedResult, content map[string]any) evaluatedResult {
	status, allergy := allergyStatus(content)
	result.Evidence["field"] = "allergy_history.status"
	result.Evidence["status"] = status
	result.Evidence["allergy_history"] = allergy
	switch status {
	case "无药物过敏", "具体药物过敏", "其他过敏原", "尚未询问", "待确认":
		return passResult(result, "过敏史状态已记录。")
	default:
		return failResult(result, "过敏史状态未填写或不合法。", "请确认并选择过敏史状态。")
	}
}

func allergyConfirmedResult(result evaluatedResult, content map[string]any) evaluatedResult {
	status, allergy := allergyStatus(content)
	result.Evidence["field"] = "allergy_history.status"
	result.Evidence["status"] = status
	result.Evidence["allergy_history"] = allergy
	if status == "无药物过敏" || status == "具体药物过敏" || status == "其他过敏原" {
		return passResult(result, "过敏史已完成确认。")
	}
	return failResult(result, "过敏史仍为尚未询问或待确认。", "请完成过敏史问诊和确认。")
}

func allergyDetailResult(result evaluatedResult, content map[string]any, expectedStatus, detailKey, suggested string) evaluatedResult {
	status, allergy := allergyStatus(content)
	detail := ""
	if value, ok := allergy[detailKey]; ok && value != nil {
		detail = strings.TrimSpace(fmt.Sprint(value))
	}
	result.Evidence["field"] = "allergy_history." + detailKey
	result.Evidence["status"] = status
	result.Evidence["detail"] = detail
	if status != expectedStatus || detail != "" {
		return passResult(result, "过敏原信息完整。")
	}
	return failResult(result, "过敏原信息缺失。", suggested)
}

func allergyStatus(content map[string]any) (string, map[string]any) {
	value, ok := content["allergy_history"]
	if !ok || value == nil {
		return "", map[string]any{}
	}
	if object, ok := value.(map[string]any); ok {
		status := strings.TrimSpace(fmt.Sprint(object["status"]))
		if status == "<nil>" {
			status = ""
		}
		return status, object
	}
	status := strings.TrimSpace(fmt.Sprint(value))
	if status == "<nil>" {
		status = ""
	}
	return status, map[string]any{"status": status}
}

func allergyPrescriptionRiskResult(result evaluatedResult, data checkContext) evaluatedResult {
	status, allergy := allergyStatus(data.Content)
	result.Evidence["allergy_status"] = status
	result.Evidence["allergy_history"] = allergy
	if status == "" || status == "尚未询问" || status == "待确认" {
		return incompleteResult(result, "过敏史状态未完成，无法判断处方风险。", "请先完成过敏史确认。")
	}
	if status == "无药物过敏" || status == "其他过敏原" {
		return passResult(result, "当前没有明确的药物过敏原需要与处方匹配。")
	}
	if status != "具体药物过敏" {
		return incompleteResult(result, "过敏史状态无法用于处方风险判断。", "请修正过敏史状态后重新检查。")
	}

	conflict, hasConflict := explicitAllergyConflict(data.Content, data.EncounterContext, data.RecordContext)
	result.Evidence["explicit_conflict_result"] = conflict
	result.Evidence["has_explicit_conflict_result"] = hasConflict
	if !hasConflict {
		return incompleteResult(result, "缺少过敏原与处方药品的明确匹配结果，无法判断。", "请接入药品成分匹配结果后重新检查，或人工复核处方风险。")
	}
	if conflict {
		return failResult(result, "已知药物过敏与处方存在明确匹配冲突。", "请立即核对过敏史和处方，确认是否更换或人工复核。")
	}
	return passResult(result, "已知药物过敏与处方没有明确匹配冲突。")
}

func explicitAllergyConflict(contexts ...map[string]any) (bool, bool) {
	for _, context := range contexts {
		for _, key := range []string{
			"allergy_prescription_conflict",
			"known_allergy_prescription_conflict",
			"drug_allergy_conflict",
		} {
			value, ok := context[key]
			if !ok {
				continue
			}
			switch typed := value.(type) {
			case bool:
				return typed, true
			case string:
				normalized := strings.ToLower(strings.TrimSpace(typed))
				switch normalized {
				case "true", "yes", "是", "冲突":
					return true, true
				case "false", "no", "否", "无冲突":
					return false, true
				}
			}
		}
	}
	return false, false
}

func requiredResult(result evaluatedResult, content map[string]any, code, label string) evaluatedResult {
	result.Evidence["field"] = code
	value := textValue(content, code)
	result.Evidence["value_present"] = strings.TrimSpace(value) != ""
	if strings.TrimSpace(value) == "" {
		return failResult(result, label+"没有填写内容。", "请补充"+label+"。")
	}
	return passResult(result, label+"已填写。")
}

func passResult(result evaluatedResult, explanation string) evaluatedResult {
	result.Conclusion = "符合"
	result.CheckStatus = "已完成"
	result.HandlingResult = "允许继续"
	result.Explanation = explanation
	return result
}

func failResult(result evaluatedResult, explanation, suggested string) evaluatedResult {
	result.Conclusion = "不符合"
	result.CheckStatus = "已完成"
	result.Explanation = explanation
	result.Suggested = suggested
	return result
}

func textValue(content map[string]any, code string) string {
	value, ok := content[code]
	if !ok {
		return ""
	}
	if object, ok := value.(map[string]any); ok {
		if nested, ok := object["value"]; ok {
			return fmt.Sprint(nested)
		}
	}
	return fmt.Sprint(value)
}

func objectValue(content map[string]any, code string) map[string]any {
	value, ok := content[code]
	if !ok {
		return map[string]any{}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	if nested, ok := object["value"].(map[string]any); ok {
		return nested
	}
	return object
}

func notApplicable(code string, data checkContext) bool {
	if hasPrefix(code, "emr.gynecology.", "gynecology.") && data.Specialty != "妇科" {
		return true
	}
	if hasPrefix(code, "emr.tcm.", "tcm.") && data.Specialty != "中医" {
		return true
	}
	if hasPrefix(code, "emr.dental.", "dental.") && data.Specialty != "口腔" {
		return true
	}
	if hasPrefix(code, "emr.pediatrics.", "pediatrics.") && data.Specialty != "儿科" {
		return true
	}
	if (code == "emr.present_illness.revisit_change" || code == "emr.present_illness.followup_change") && data.VisitType != "复诊" {
		return true
	}
	return false
}

func hasPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func handlingForNonconforming(deadline, trigger string) string {
	if deadlineDue(deadline, trigger) {
		return "阻断"
	}
	return "待医生处理"
}

func handlingForIncomplete(deadline, trigger string) string {
	if deadlineDue(deadline, trigger) {
		return "阻断"
	}
	return "待人工判断"
}

func deadlineDue(deadline, trigger string) bool {
	if deadline == "仅提示" || deadline == "归档后质控" || trigger == "手动保存" {
		return false
	}
	rank := map[string]int{"提交": 1, "确认": 2, "归档": 3, "人工重新检查": 3}
	deadlineRank := map[string]int{"提交前处理": 1, "确认前处理": 2, "归档前处理": 3}
	return deadlineRank[deadline] > 0 && rank[trigger] >= deadlineRank[deadline]
}

func boolPtr(value bool) *bool { return &value }
