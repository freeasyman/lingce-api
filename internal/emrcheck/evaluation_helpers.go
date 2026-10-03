package emrcheck

import (
	"fmt"
	"strings"
)

// normalizeEvaluation is the single normalization boundary for all executor
// output. Individual executors only decide the clinical/program fact; this
// function supplies the stable persisted status contract.
func normalizeEvaluation(result *evaluatedResult) {
	switch result.ExecutionStatus {
	case "incomplete":
		result.ExecutionStatus = "manual_required"
	case "":
		if result.CheckStatus == "已完成" {
			result.ExecutionStatus = "completed"
		} else {
			result.ExecutionStatus = "manual_required"
		}
	}

	switch result.Conclusion {
	case "未判断", "":
		result.Conclusion = "无法判断"
	}
	switch result.CheckStatus {
	case "未判断", "":
		result.CheckStatus = "未完成"
	}
	switch result.HandlingResult {
	case "跳过":
		result.HandlingResult = "待人工判断"
	case "":
		result.HandlingResult = "待人工判断"
	}

	if result.CheckStatus == "已完成" {
		result.IncompleteReason = ""
		return
	}
	switch result.ExecutionStatus {
	case "executor_not_implemented":
		result.IncompleteReason = "程序执行器未实现"
	case "execution_failed":
		result.IncompleteReason = "执行失败"
	case "manual_required":
		result.IncompleteReason = "人工待处理"
	default:
		switch result.IncompleteReason {
		case "执行失败", "人工待处理", "程序执行器未实现", "执行器未调用":
			return
		default:
			result.IncompleteReason = "人工待处理"
		}
	}
}

func passResult(result evaluatedResult, explanation string) evaluatedResult {
	result.Conclusion = "符合"
	result.CheckStatus = "已完成"
	result.ExecutionStatus = "completed"
	result.HandlingResult = "允许继续"
	result.Explanation = explanation
	if strings.Contains(explanation, "规则不适用") {
		if result.Evidence == nil {
			result.Evidence = map[string]any{}
		}
		required, _ := result.Evidence["field_required"].(bool)
		switch {
		case strings.Contains(explanation, "为空") && required:
			result.Evidence["applicability"] = "empty_required_not_checked"
		case strings.Contains(explanation, "为空"):
			result.Evidence["applicability"] = "empty_optional_not_checked"
		default:
			result.Evidence["applicability"] = "condition_not_triggered"
		}
	}
	return result
}

func failResult(result evaluatedResult, explanation, suggested string) evaluatedResult {
	result.Conclusion = "不符合"
	result.CheckStatus = "已完成"
	result.ExecutionStatus = "completed"
	result.Explanation = explanation
	result.Suggested = suggested
	return result
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

func textValue(content map[string]any, code string) string {
	value, ok := content[code]
	if !ok || value == nil {
		return ""
	}
	if object, ok := value.(map[string]any); ok {
		if nested, ok := object["value"]; ok && nested != nil {
			return fmt.Sprint(nested)
		}
	}
	return fmt.Sprint(value)
}

func diagnosisNames(content map[string]any) []string {
	value, ok := content["diagnosis"]
	if !ok {
		return nil
	}
	return compactUniqueStrings(diagnosisNamesFromValue(value))
}

func diagnosisNamesFromValue(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []string{typed}
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, diagnosisNamesFromValue(item)...)
		}
		return items
	case []map[string]any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, diagnosisNamesFromValue(item)...)
		}
		return items
	case map[string]any:
		for _, key := range []string{"diagnosis_name", "name", "value", "text"} {
			if nested, ok := typed[key]; ok {
				return diagnosisNamesFromValue(nested)
			}
		}
	}
	return []string{fmt.Sprint(value)}
}

func compactUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		items = append(items, value)
	}
	return items
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
