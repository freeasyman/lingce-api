package emrcheck

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ProgramExecutor 程序执行器函数签名
type ProgramExecutor func(rule ExecutableRule, data checkContext) evaluatedResult

// ExecutorRegistry 执行器注册表
type ExecutorRegistry struct {
	executors map[string]ProgramExecutor
}

// NewExecutorRegistry 创建新的执行器注册表
func NewExecutorRegistry() *ExecutorRegistry {
	registry := &ExecutorRegistry{
		executors: make(map[string]ProgramExecutor),
	}
	registry.registerAllExecutors()
	return registry
}

// Execute 执行指定的规则
func (r *ExecutorRegistry) Execute(rule ExecutableRule, data checkContext) evaluatedResult {
	// 基础结果模板
	result := evaluatedResult{
		Conclusion:      "无法判断",
		CheckStatus:     "未完成",
		ExecutionStatus: "completed",
		HandlingResult:  "待人工判断",
		Evidence: map[string]any{
			"snapshot_id":    data.SnapshotID,
			"field_code":     rule.FieldCode,
			"field_name":     rule.FieldName,
			"field_required": rule.FieldRequired,
			"rule_code":      rule.RuleCode,
		},
		ActualMode: rule.Execution,
	}

	// 查找执行器
	executor, exists := r.executors[rule.Executor]
	if !exists {
		result.Conclusion = "无法完成"
		result.CheckStatus = "未完成"
		result.ExecutionStatus = "executor_not_implemented"
		result.IncompleteReason = "程序执行器未实现"
		result.HandlingResult = "系统配置缺失"
		result.Explanation = fmt.Sprintf("执行器 %s 尚未实现。", rule.Executor)
		result.Suggested = "请补充该规则的程序执行器后重新检查。"
		result.Evidence["executor"] = rule.Executor
		result.Evidence["reason"] = "program_executor_not_implemented"
		return result
	}

	// 执行
	return executor(rule, data)
}

// Has reports whether a configured program executor is registered.
func (r *ExecutorRegistry) Has(name string) bool {
	_, ok := r.executors[name]
	return ok
}

// Names returns the registered executor names in no particular order.
// It is used by startup/configuration validation and tests.
func (r *ExecutorRegistry) Names() []string {
	names := make([]string, 0, len(r.executors))
	for name := range r.executors {
		names = append(names, name)
	}
	return names
}

// registerAllExecutors 注册所有执行器
func (r *ExecutorRegistry) registerAllExecutors() {
	// 这里只注册真正由程序执行的规则。大模型规则由服务层统一收集，
	// 不再把大模型占位函数注册为程序执行器。
	r.register("chief_complaint_required", chiefComplaintRequired)
	r.register("chief_complaint_max_length", chiefComplaintMaxLength)
	// 过敏史 allergy_history
	r.register("allergy_history_status_valid", allergyHistoryStatusValid)
	r.register("allergy_history_unconfirmed", allergyHistoryUnconfirmed)
	r.register("allergy_history_drug_detail", allergyHistoryDrugDetail)
	r.register("allergy_history_other_detail", allergyHistoryOtherDetail)
	// 体格检查 physical_exam
	r.register("physical_exam_vital_signs_format", physicalExamVitalSignsFormat)
	// 诊断 diagnosis
	r.register("diagnosis_present", diagnosisPresent)
	r.register("diagnosis_icd10_match", diagnosisIcd10Match)
	// 随访、妇科、产科和儿科格式规则
	r.register("followup_date_format", followupDateFormat)
	r.register("last_menstrual_period_valid_date", lastMenstrualPeriodValidDate)
	r.register("obstetric_history_numbers_format", obstetricHistoryNumbersFormat)
	r.register("pediatric_weight_value_and_unit", pediatricWeightValueAndUnit)
}

// register 注册单个执行器
func (r *ExecutorRegistry) register(name string, executor ProgramExecutor) {
	r.executors[name] = executor
}

// ===== 主诉执行器实现 =====

// chiefComplaintRequired 主诉不能为空
func chiefComplaintRequired(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)

	value := textValue(data.Content, "chief_complaint")
	result.Evidence["field"] = "chief_complaint"
	result.Evidence["value"] = value
	result.Evidence["length"] = utf8.RuneCountInString(value)

	if strings.TrimSpace(value) == "" {
		return failResult(result, "主诉为空。", "请填写患者本次就诊的主要问题。")
	}

	return passResult(result, "主诉已填写。")
}

// chiefComplaintMaxLength 主诉不超过20字
func chiefComplaintMaxLength(rule ExecutableRule, data checkContext) evaluatedResult {
	result := baseResult(rule, data)

	value := textValue(data.Content, "chief_complaint")
	length := utf8.RuneCountInString(value)

	// 从参数中获取最大长度
	maxLength := 20
	if val, ok := rule.Parameters["max_length"].(float64); ok {
		maxLength = int(val)
	}

	result.Evidence["field"] = "chief_complaint"
	result.Evidence["value"] = value
	result.Evidence["length"] = length
	result.Evidence["max_length"] = maxLength

	if strings.TrimSpace(value) == "" {
		return failResult(result, "主诉为空,无法满足字数要求。", "请先填写主诉。")
	}

	if length <= maxLength {
		return passResult(result, fmt.Sprintf("主诉不超过%d个字符。", maxLength))
	}

	return failResult(result,
		fmt.Sprintf("主诉超过%d个字符(当前%d字符)。", maxLength, length),
		"请压缩主诉内容。")
}

// ===== 辅助函数 =====

// baseResult 创建基础结果
func baseResult(rule ExecutableRule, data checkContext) evaluatedResult {
	return evaluatedResult{
		Conclusion:      "无法判断",
		CheckStatus:     "未完成",
		ExecutionStatus: "completed",
		HandlingResult:  "待人工判断",
		Evidence: map[string]any{
			"snapshot_id":    data.SnapshotID,
			"field_code":     rule.FieldCode,
			"field_name":     rule.FieldName,
			"field_required": rule.FieldRequired,
			"rule_code":      rule.RuleCode,
		},
		ActualMode: "程序判断",
	}
}

// 注意: passResult, failResult, textValue, boolPtr 函数已在 evaluator.go 中定义
