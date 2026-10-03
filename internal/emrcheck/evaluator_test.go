package emrcheck

import "testing"

func TestFilterLLMRulesByProgramResults(t *testing.T) {
	rules := []qualityRuleInput{
		{Code: "duration", FieldCode: "chief_complaint", SkipIf: []RuleDependency{{FieldCode: "chief_complaint", RuleCode: "required", Conclusion: "不符合"}}},
		{Code: "symptom_sign", FieldCode: "chief_complaint", SkipIf: []RuleDependency{{FieldCode: "chief_complaint", RuleCode: "required", Conclusion: "不符合"}}},
		{Code: "history_quality", FieldCode: "present_illness"},
	}
	program := []*CheckResult{
		{FieldCode: "chief_complaint", RuleCode: "required", Conclusion: "不符合", CheckStatus: "已完成"},
	}
	filtered := filterLLMRulesByProgramResults(rules, program)
	if len(filtered) != 1 || filtered[0].Code != "history_quality" {
		t.Fatalf("filtered rules = %#v", filtered)
	}

	program[0].Conclusion = "符合"
	filtered = filterLLMRulesByProgramResults(rules, program)
	if len(filtered) != 3 {
		t.Fatalf("a passing prerequisite must not skip rules: %#v", filtered)
	}

	program[0].Conclusion = "不符合"
	program[0].CheckStatus = "未完成"
	filtered = filterLLMRulesByProgramResults(rules, program)
	if len(filtered) != 3 {
		t.Fatalf("an incomplete prerequisite must not skip rules: %#v", filtered)
	}
}

func TestMergeQualityResultsOnlyMergesDeclaredFacts(t *testing.T) {
	results := mergeQualityResults([]*CheckResult{
		{FieldCode: "chief_complaint", FactCode: "duration", RuleCode: "duration", ActualExecutionMode: "大模型判断", Conclusion: "符合", CheckStatus: "已完成"},
		{FieldCode: "chief_complaint", FactCode: "duration", RuleCode: "duration_program", ActualExecutionMode: "程序判断", Conclusion: "不符合", CheckStatus: "已完成", HandlingResult: "阻断"},
		{FieldCode: "chief_complaint", RuleCode: "not_vague", Conclusion: "不符合", CheckStatus: "已完成"},
	})
	if len(results) != 2 {
		t.Fatalf("merged result count = %d, want 2", len(results))
	}
	if results[0].ActualExecutionMode != "程序判断" || results[0].Conclusion != "不符合" {
		t.Fatalf("program result should win: %+v", results[0])
	}
}

func TestConfiguredProgramExecutorsAreRegistered(t *testing.T) {
	registry := NewExecutorRegistry()
	configured := []string{
		"chief_complaint_required",
		"allergy_history_status_valid",
		"diagnosis_icd10_match",
	}
	for _, name := range configured {
		if !registry.Has(name) {
			t.Fatalf("configured executor %q is not registered", name)
		}
	}
}

func executeProgramRuleForTest(rule ExecutableRule, content map[string]any, trigger string) evaluatedResult {
	data := checkContext{
		SnapshotID: "snapshot-test",
		Content:    content,
		VisitType:  "初诊",
		Specialty:  "全科",
	}
	result := NewExecutorRegistry().Execute(rule, data)
	normalizeFieldEvaluation(rule, &result, data, trigger)
	return result
}

func TestProgramRuleUsesFieldExecutorRegistry(t *testing.T) {
	rule := ExecutableRule{
		FieldCode:  "chief_complaint",
		FieldName:  "主诉",
		RuleCode:   "required",
		RuleName:   "主诉不能为空",
		Execution:  "program",
		Executor:   "chief_complaint_required",
		Parameters: map[string]any{},
	}

	result := executeProgramRuleForTest(rule, map[string]any{}, "归档")
	if result.Conclusion != "不符合" || result.HandlingResult != "阻断" {
		t.Fatalf("empty chief complaint: conclusion=%q handling=%q", result.Conclusion, result.HandlingResult)
	}

	result = executeProgramRuleForTest(rule, map[string]any{"chief_complaint": "咳嗽3天"}, "归档")
	if result.Conclusion != "符合" || result.HandlingResult != "允许继续" {
		t.Fatalf("filled chief complaint: conclusion=%q handling=%q", result.Conclusion, result.HandlingResult)
	}
}

func TestAllergyUsesStructuredStatusOnly(t *testing.T) {
	rule := ExecutableRule{
		FieldCode: "allergy_history",
		FieldName: "过敏史",
		RuleCode:  "status_valid",
		RuleName:  "过敏史状态合法",
		Execution: "program",
		Executor:  "allergy_history_status_valid",
		Parameters: map[string]any{
			"allowed_statuses": []any{"无药物过敏", "具体药物过敏", "其他过敏原", "尚未询问", "待确认"},
		},
	}

	result := executeProgramRuleForTest(rule, map[string]any{
		"allergy_history": map[string]any{
			"status":         "其他过敏原",
			"other_allergen": "海鲜过敏",
		},
	}, "提交")
	if result.Conclusion != "符合" {
		t.Fatalf("structured other allergen should pass: %+v", result)
	}

	result = executeProgramRuleForTest(rule, map[string]any{
		"allergy_history": "海鲜过敏",
	}, "提交")
	if result.Conclusion != "不符合" {
		t.Fatalf("free text allergy value must not infer status: %+v", result)
	}
}

func TestAllergyDetailAppliesOnlyToSelectedStatus(t *testing.T) {
	rule := ExecutableRule{
		FieldCode: "allergy_history",
		FieldName: "过敏史",
		RuleCode:  "other_detail",
		RuleName:  "其他过敏原已记录",
		Execution: "program",
		Executor:  "allergy_history_other_detail",
		Parameters: map[string]any{
			"required_when_status": "其他过敏原",
			"required_key":         "other_allergen",
		},
	}

	result := executeProgramRuleForTest(rule, map[string]any{
		"allergy_history": map[string]any{
			"status": "其他过敏原",
		},
	}, "提交")
	if result.Conclusion != "不符合" {
		t.Fatalf("missing other allergen detail should fail: %+v", result)
	}

	result = executeProgramRuleForTest(rule, map[string]any{
		"allergy_history": map[string]any{
			"status":         "其他过敏原",
			"other_allergen": "海鲜过敏",
		},
	}, "提交")
	if result.Conclusion != "符合" {
		t.Fatalf("other allergen detail should pass: %+v", result)
	}

	result = executeProgramRuleForTest(rule, map[string]any{
		"allergy_history": map[string]any{
			"status": "无药物过敏",
		},
	}, "提交")
	if result.Conclusion != "符合" {
		t.Fatalf("other detail must not apply to no-drug-allergy: %+v", result)
	}
}

func TestFollowupDateFormatRequiresValidDateAndAllowsNotes(t *testing.T) {
	rule := ExecutableRule{
		FieldCode:  "followup",
		FieldName:  "复诊安排",
		RuleCode:   "date_format",
		RuleName:   "复诊日期格式有效",
		Execution:  "program",
		Executor:   "followup_date_format",
		Parameters: map[string]any{},
	}

	cases := []struct {
		name       string
		value      string
		conclusion string
	}{
		{name: "date only", value: "2026-10-01", conclusion: "符合"},
		{name: "date with note", value: "2026-10-01 两周后复诊", conclusion: "符合"},
		{name: "chinese date with note", value: "2026年10月1日复查", conclusion: "符合"},
		{name: "relative date only", value: "下周复诊", conclusion: "不符合"},
		{name: "invalid date", value: "2026-99-99复诊", conclusion: "不符合"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := executeProgramRuleForTest(rule, map[string]any{"followup": tc.value}, "提交")
			if result.Conclusion != tc.conclusion {
				t.Fatalf("conclusion=%q, want %q, result=%+v", result.Conclusion, tc.conclusion, result)
			}
		})
	}
}

func TestLastMenstrualPeriodValidDateAllowsUnknownButRejectsInvalidDate(t *testing.T) {
	rule := ExecutableRule{
		FieldCode:  "last_menstrual_period",
		FieldName:  "末次月经",
		RuleCode:   "valid_date",
		RuleName:   "末次月经日期合法",
		Execution:  "program",
		Executor:   "last_menstrual_period_valid_date",
		Parameters: map[string]any{},
	}

	cases := []struct {
		name       string
		value      string
		conclusion string
	}{
		{name: "valid date", value: "2026-10-01", conclusion: "符合"},
		{name: "unknown", value: "不详", conclusion: "符合"},
		{name: "unclear", value: "记不清", conclusion: "符合"},
		{name: "invalid date", value: "2026-99-99", conclusion: "不符合"},
		{name: "free text", value: "上个月", conclusion: "不符合"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := executeProgramRuleForTest(rule, map[string]any{"last_menstrual_period": tc.value}, "提交")
			if result.Conclusion != tc.conclusion {
				t.Fatalf("conclusion=%q, want %q, result=%+v", result.Conclusion, tc.conclusion, result)
			}
		})
	}
}

func TestPediatricWeightValueAndUnitAllowsKilogramOnly(t *testing.T) {
	rule := ExecutableRule{
		FieldCode:  "pediatric_weight",
		FieldName:  "儿童体重",
		RuleCode:   "value_and_unit",
		RuleName:   "儿童体重有明确数值和单位",
		Execution:  "program",
		Executor:   "pediatric_weight_value_and_unit",
		Parameters: map[string]any{},
	}

	cases := []struct {
		name       string
		value      string
		conclusion string
	}{
		{name: "kg lower", value: "15kg", conclusion: "符合"},
		{name: "kg upper", value: "15KG", conclusion: "符合"},
		{name: "gongjin", value: "15公斤", conclusion: "符合"},
		{name: "qianke", value: "15千克", conclusion: "符合"},
		{name: "jin", value: "30斤", conclusion: "不符合"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := executeProgramRuleForTest(rule, map[string]any{"pediatric_weight": tc.value}, "提交")
			if result.Conclusion != tc.conclusion {
				t.Fatalf("conclusion=%q, want %q, result=%+v", result.Conclusion, tc.conclusion, result)
			}
		})
	}
}
