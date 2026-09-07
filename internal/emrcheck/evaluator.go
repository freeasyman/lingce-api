package emrcheck

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type binding struct {
	QualityRequirementID string
	Code                 string
	Name                 string
	RuleType             string
	ExecutionMode        string
	DeadlineAction       string
}

type checkContext struct {
	RecordID          string
	SnapshotID        string
	TemplateVersionID string
	VisitType         string
	Specialty         string
	Content           map[string]any
	ConfirmedBy       *int64
}

type evaluatedResult struct {
	Conclusion       string
	CheckStatus      string
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
		Conclusion:     "无法判断",
		CheckStatus:    "未完成",
		HandlingResult: "待人工判断",
		Evidence:       map[string]any{"snapshot_id": data.SnapshotID},
		ActualMode:     item.ExecutionMode,
	}

	if notApplicable(item.Code, data) {
		result.Conclusion = "符合"
		result.CheckStatus = "已完成"
		result.HandlingResult = "允许继续"
		result.MeetsDeadline = boolPtr(true)
		result.Evidence["前提"] = "当前病历不具备该要求的事实前提"
		result.Explanation = "当前病历不适用此项要求。"
		return result
	}

	if item.ExecutionMode != "程序判断" {
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
	result.ActualMode = "程序判断"
	if result.CheckStatus == "已完成" {
		result.MeetsDeadline = boolPtr(true)
		if result.Conclusion == "不符合" {
			result.MeetsDeadline = boolPtr(false)
			result.HandlingResult = handlingForNonconforming(item.DeadlineAction, trigger)
		} else {
			result.HandlingResult = "允许继续"
		}
	} else {
		result.MeetsDeadline = boolPtr(false)
		result.HandlingResult = handlingForIncomplete(item.DeadlineAction, trigger)
	}
	return result
}

func evaluateProgram(item binding, data checkContext) evaluatedResult {
	result := evaluatedResult{
		Conclusion:     "无法判断",
		CheckStatus:    "未完成",
		HandlingResult: "待人工判断",
		Evidence:       map[string]any{"snapshot_id": data.SnapshotID},
	}

	switch item.Code {
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
	case "emr.allergy.required.unconfirmed":
		return allergyConfirmedResult(result, data.Content)
	case "emr.allergy.required.drug_detail":
		return allergyDetailResult(result, data.Content, "具体药物过敏", "drug_name", "请补充具体的过敏药物名称。")
	case "emr.allergy.required.other_detail":
		return allergyDetailResult(result, data.Content, "其他过敏原", "other_allergen", "请补充其他过敏原说明。")
	case "emr.diagnosis.present":
		return requiredResult(result, data.Content, "diagnosis", "诊断")
	case "emr.signature.doctor":
		result.Evidence["confirmed_by"] = data.ConfirmedBy
		if data.ConfirmedBy != nil && *data.ConfirmedBy > 0 {
			return passResult(result, "病历已有有效责任确认。")
		}
		return failResult(result, "尚未找到责任医生确认记录。", "请由责任医生确认病历。")
	default:
		result.IncompleteReason = "人工待处理"
		result.Explanation = "当前版本没有为这条质量要求配置确定性程序判断。"
		result.Suggested = "转人工判断或接入相应执行器后重新检查。"
		return result
	}
}

func allergyStatusResult(result evaluatedResult, content map[string]any) evaluatedResult {
	allergy := objectValue(content, "allergy_history")
	status, _ := allergy["status"].(string)
	status = strings.TrimSpace(status)
	result.Evidence["field"] = "allergy_history.status"
	result.Evidence["status"] = status
	switch status {
	case "无药物过敏", "具体药物过敏", "其他过敏原", "尚未询问", "待确认":
		return passResult(result, "过敏史状态已记录。")
	default:
		return failResult(result, "过敏史状态未填写或不合法。", "请确认并选择过敏史状态。")
	}
}

func allergyConfirmedResult(result evaluatedResult, content map[string]any) evaluatedResult {
	allergy := objectValue(content, "allergy_history")
	status, _ := allergy["status"].(string)
	status = strings.TrimSpace(status)
	result.Evidence["field"] = "allergy_history.status"
	result.Evidence["status"] = status
	if status == "无药物过敏" || status == "具体药物过敏" || status == "其他过敏原" {
		return passResult(result, "过敏史已完成确认。")
	}
	return failResult(result, "过敏史仍为尚未询问或待确认。", "请完成过敏史问诊和确认。")
}

func allergyDetailResult(result evaluatedResult, content map[string]any, expectedStatus, detailKey, suggested string) evaluatedResult {
	allergy := objectValue(content, "allergy_history")
	status, _ := allergy["status"].(string)
	detail := ""
	if value, ok := allergy[detailKey]; ok && value != nil {
		detail = strings.TrimSpace(fmt.Sprint(value))
	}
	result.Evidence["field"] = "allergy_history." + detailKey
	result.Evidence["status"] = status
	if status != expectedStatus || detail != "" {
		return passResult(result, "过敏原信息完整。")
	}
	return failResult(result, "过敏原信息缺失。", suggested)
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
