package emrcheck

import (
	"fmt"
	"strings"
	"time"
)

// evaluateTimeConsistency 执行病历时间链的确定性校验。
//
// 只比较输入中确实存在、且能够解析的时间节点。提交、确认或归档尚未
// 发生时，不把这些未来节点当成缺失；但当前状态明确要求的节点缺失时，
// 会返回未完成。异常跨度必须依赖外部配置，当前上下文没有阈值时不猜
// 一个数字，而是明确报告无法完成。
//
// 署名：Codex
// 时间：2026-09-14
func evaluateTimeConsistency(code string, result evaluatedResult, data checkContext) evaluatedResult {
	nodes := timeNodes(data)
	result.Evidence["time_nodes"] = nodes
	switch code {
	case "emr.time.consistency.fields_present":
		required := []string{"created_at", "started_at", "last_saved_at"}
		switch data.Status {
		case "已提交":
			required = append(required, "ended_at", "submitted_at")
		case "已确认":
			required = append(required, "ended_at", "submitted_at")
		case "已归档":
			required = append(required, "ended_at", "submitted_at", "archived_at")
		}
		missing := make([]string, 0)
		for _, name := range required {
			if _, ok := nodes[name]; !ok {
				missing = append(missing, name)
			}
		}
		result.Evidence["required_nodes"] = required
		result.Evidence["missing_nodes"] = missing
		if len(missing) > 0 {
			return incompleteResult(result, "当前状态所需的时间节点缺失。", "请补齐病历时间节点。")
		}
		return passResult(result, "当前状态所需的时间节点均已存在。")
	case "emr.time.consistency.order":
		order := []string{"created_at", "started_at", "ended_at", "submitted_at", "archived_at"}
		var previousName string
		var previous time.Time
		for _, name := range order {
			value, ok := nodes[name]
			if !ok {
				continue
			}
			if previousName != "" && value.Before(previous) {
				result.Evidence["conflict"] = map[string]any{
					"previous_node": previousName, "previous_time": previous,
					"current_node": name, "current_time": value,
				}
				return failResult(result, fmt.Sprintf("%s早于%s。", name, previousName), "请修正病历时间顺序。")
			}
			previousName, previous = name, value
		}
		return passResult(result, "已存在的病历时间节点顺序正确。")
	case "emr.time.consistency.last_modified":
		lastModified, modifiedOK := nodes["updated_at"]
		lastSaved, savedOK := nodes["last_saved_at"]
		created, createdOK := nodes["created_at"]
		result.Evidence["updated_at"] = lastModified
		result.Evidence["last_saved_at"] = lastSaved
		if !modifiedOK || !savedOK || !createdOK {
			return incompleteResult(result, "缺少创建、最后保存或最后修改时间，无法判断。", "请补齐病历时间记录后重新检查。")
		}
		if lastModified.Before(created) || lastModified.Before(lastSaved) {
			return failResult(result, "最后修改时间早于创建或最后保存时间。", "请修正最后修改时间记录。")
		}
		return passResult(result, "最后修改时间顺序有效。")
	case "emr.time.consistency.abnormal_span":
		threshold := lookupDurationSeconds(data.RecordContext, data.EncounterContext)
		start, startOK := nodes["started_at"]
		end, endOK := nodes["ended_at"]
		if threshold <= 0 {
			return incompleteResult(result, "机构未配置异常就诊时长阈值，无法判断。", "请配置机构异常时长阈值后重新检查。")
		}
		if !startOK || !endOK {
			return incompleteResult(result, "缺少接诊开始或结束时间，无法判断就诊时长。", "请补齐接诊开始和结束时间。")
		}
		duration := end.Sub(start).Seconds()
		result.Evidence["duration_seconds"] = duration
		result.Evidence["threshold_seconds"] = threshold
		if duration > threshold {
			return failResult(result, "就诊时长超过机构配置阈值。", "请核对接诊时间和异常时长原因。")
		}
		return passResult(result, "就诊时长未超过机构配置阈值。")
	default:
		result.ExecutionStatus = "executor_not_implemented"
		result.Conclusion = "无法完成"
		result.CheckStatus = "未完成"
		result.IncompleteReason = "时间一致性执行器未实现"
		result.HandlingResult = "系统配置缺失"
		result.Explanation = fmt.Sprintf("时间一致性规则未实现：%s。", code)
		return result
	}
}

func incompleteResult(result evaluatedResult, explanation, suggested string) evaluatedResult {
	result.Conclusion = "无法判断"
	result.CheckStatus = "未完成"
	result.ExecutionStatus = "completed"
	result.HandlingResult = "待人工判断"
	result.IncompleteReason = explanation
	result.Explanation = explanation
	result.Suggested = suggested
	return result
}

func timeNodes(data checkContext) map[string]time.Time {
	nodes := make(map[string]time.Time)
	add := func(name string, value *time.Time) {
		if value != nil && !value.IsZero() {
			nodes[name] = value.UTC()
		}
	}
	add("created_at", data.CreatedAt)
	add("started_at", data.StartedAt)
	add("ended_at", data.EndedAt)
	add("submitted_at", data.SubmittedAt)
	add("archived_at", data.ArchivedAt)
	add("last_saved_at", data.LastSavedAt)
	add("updated_at", data.UpdatedAt)
	return nodes
}

func lookupDurationSeconds(contexts ...map[string]any) float64 {
	for _, context := range contexts {
		for _, key := range []string{"abnormal_span_seconds", "abnormal_duration_seconds", "max_encounter_duration_seconds"} {
			if value, ok := context[key]; ok {
				switch typed := value.(type) {
				case float64:
					return typed
				case int:
					return float64(typed)
				case int64:
					return float64(typed)
				case string:
					var seconds float64
					if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%f", &seconds); err == nil {
						return seconds
					}
				}
			}
		}
	}
	return 0
}
