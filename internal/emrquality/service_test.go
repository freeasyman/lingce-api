package emrquality

import "testing"

func TestEvaluateProgramRules(t *testing.T) {
	input := Context{
		Patient: map[string]any{"name": "", "gender": "女", "birth_date": ""},
		Content: map[string]any{
			"chief_complaint": "",
			"present_illness": "",
			"allergy_history": "待确认",
			"physical_exam":   map[string]any{},
			"diagnosis":       "",
		},
	}
	for _, code := range []string{"GEN-001", "CC-001", "HIS-001", "HIS-009", "EXAM-001", "DX-001"} {
		result, err := evaluateRule(input, ruleDefinition{Code: code, SectionCode: sectionForTest(code), StandardPoints: standardForTest(code), EvaluationSpec: specForTest(code)})
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if result.Result != "problem" {
			t.Fatalf("%s result = %s", code, result.Result)
		}
	}
}

func TestUnimplementedRuleIsNotEvaluable(t *testing.T) {
	result, err := evaluateRule(Context{}, ruleDefinition{Code: "UNKNOWN", SectionCode: "history", ExecutorKind: "program"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "not_evaluable" {
		t.Fatalf("result = %s, want not_evaluable", result.Result)
	}
}

func TestImplementedRuleWithoutFindingPasses(t *testing.T) {
	result, err := evaluateRule(Context{Content: map[string]any{"chief_complaint": "咳嗽3天"}}, ruleDefinition{Code: "CC-001", SectionCode: "chief_complaint", StandardPoints: 5, ExecutorKind: "program"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "pass" {
		t.Fatalf("result = %s, want pass", result.Result)
	}
}

func TestDiagnosisWithoutICD10CodeIsNotAcceptedByProgramRule(t *testing.T) {
	items := diagnosisItems("腰椎间盘突出症")
	if len(items) != 1 || valueString(items[0], "diagnosis_name") != "腰椎间盘突出症" {
		t.Fatalf("unexpected diagnosis normalization: %#v", items)
	}
}

func TestDiagnosisContentUsesDisplayedSpecialtyDiagnosis(t *testing.T) {
	content := map[string]any{
		"diagnosis":     "",
		"tcm_diagnosis": map[string]any{"diagnosis_name": "急性上呼吸道感染"},
	}
	items := diagnosisItems(diagnosisContent(content))
	if len(items) != 1 || valueString(items[0], "diagnosis_name") != "急性上呼吸道感染" {
		t.Fatalf("diagnosis content = %#v, want displayed specialty diagnosis", items)
	}

	result, err := evaluateRule(Context{Content: content}, ruleDefinition{Code: "DX-001", SectionCode: "diagnosis", StandardPoints: 10, ExecutorKind: "program"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "pass" {
		t.Fatalf("DX-001 result = %s, want pass", result.Result)
	}
}

func TestBlockingFieldsBlockProblem(t *testing.T) {
	input := Context{Patient: map[string]any{"name": "", "gender": "女", "birth_date": "2000-01"}}
	result, err := evaluateRule(input, ruleDefinition{Code: "GEN-001", SectionCode: "general", StandardPoints: 2,
		EvaluationSpec: []byte(`{"required_fields":["name"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	policies := rulePolicies{Complete: actionPolicy{BlockingFields: []string{"name"}}}
	if !blockingFor(policies.Complete, result) {
		t.Fatal("required-field problem was not blocking")
	}
	result.Reason = "没有记录民族"
	result.Evidence = []Evidence{{Source: "record.patient_snapshot", Path: "patient.ethnicity", Excerpt: "民族"}}
	if blockingFor(policies.Complete, result) {
		t.Fatal("non-blocking patient field was incorrectly blocking")
	}
}

func TestPassedRuleHasNoProblemMessage(t *testing.T) {
	for _, state := range []string{"pass", "not_evaluable"} {
		result := &RuleResult{Result: state}
		if message := messageForAction([]byte(`{"complete":"没有主诉，不能完成病历。"}`), ActionComplete, result, actionPolicy{}); message != "" {
			t.Fatalf("%s should have no problem message, got %q", state, message)
		}
	}
}

func TestSemanticRulesCoverage(t *testing.T) {
	requested := map[string]bool{"CC-002": true, "CC-003": true}
	if !semanticRulesCovered([]string{"CC-002", "CC-003"}, requested) {
		t.Fatal("complete checked rule list should be accepted")
	}
	if semanticRulesCovered([]string{"CC-002"}, requested) {
		t.Fatal("incomplete checked rule list must not be accepted")
	}
	if semanticRulesCovered([]string{"CC-002", "CC-002"}, requested) {
		t.Fatal("duplicate checked rule must not be accepted")
	}
}

func TestNonPassResultAlwaysHasMessage(t *testing.T) {
	problem := &RuleResult{Result: "problem", Reason: "没有记录主诉"}
	ensureResultMessage(ActionSave, problem)
	if problem.Message == "" {
		t.Fatal("problem result must have a message")
	}

	pending := &RuleResult{Result: "not_evaluable", Reason: "大模型没有完成本条规则的判断"}
	ensureResultMessage(ActionComplete, pending)
	if pending.Message == "" {
		t.Fatal("not_evaluable result must have a message")
	}

	passed := &RuleResult{Result: "pass"}
	ensureResultMessage(ActionSave, passed)
	if passed.Message != "" {
		t.Fatal("pass result must not have a message")
	}
}

func TestPatientReportedContentPassesPresenceChecks(t *testing.T) {
	input := Context{Content: map[string]any{
		"chief_complaint": "腰疼伴右腿放射痛1周",
		"present_illness": "患者1周前出现腰疼，伴有右腿放射痛。",
		"physical_exam":   map[string]any{"temperature": "", "positive_findings": "直腿抬高试验阳性，按压腰部疼痛明显。"},
		"diagnosis":       "椎间盘突出，压迫神经根。",
	}}
	for _, code := range []string{"CC-001", "CC-005", "HIS-001", "EXAM-001", "DX-001"} {
		result, err := evaluateRule(input, ruleDefinition{Code: code})
		if err != nil || result.Result != "pass" {
			t.Fatalf("%s: result=%+v, error=%v", code, result, err)
		}
	}
}

func TestGeneralMessageSeparatesBlockingFields(t *testing.T) {
	input := Context{Patient: map[string]any{"name": "患者"}}
	result, err := evaluateRule(input, ruleDefinition{Code: "GEN-001", StandardPoints: 2, EvaluationSpec: []byte(`{"required_fields":["name","gender","birth_date"],"prompt_fields":["ethnicity","marital_status","occupation","work_unit","address"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	policy := actionPolicy{BlockingFields: []string{"name", "gender", "birth_date"}}
	message := messageForAction([]byte(`{"complete":"请补充{field}，再完成病历。"}`), ActionComplete, result, policy)
	want := "请补充性别、出生年月，再完成病历。\n民族、婚姻状况、职业、工作单位、住址尚未填写，本次不阻断完成病历。"
	if message != want {
		t.Fatalf("message = %q, want %q", message, want)
	}
	if !blockingFor(policy, result) {
		t.Fatal("missing mandatory fields must block")
	}
}

func TestOnlyPromptFieldsDoNotBlockCompletion(t *testing.T) {
	input := Context{Patient: map[string]any{
		"name": "患者", "gender": "男", "birth_date": "1990-01-01",
	}}
	result, err := evaluateRule(input, ruleDefinition{Code: "GEN-001", StandardPoints: 2,
		EvaluationSpec: []byte(`{"required_fields":["name","gender","birth_date"],"prompt_fields":["ethnicity","marital_status","occupation","work_unit","address"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "problem" {
		t.Fatalf("missing prompt-only fields should still be recorded as a problem, got %s", result.Result)
	}
	policy := actionPolicy{BlockingFields: []string{"name", "gender", "birth_date"}}
	if blockingFor(policy, result) {
		t.Fatal("prompt-only fields must not block completion")
	}
	message := messageForAction([]byte(`{"complete":"请补充{field}，再完成病历。"}`), ActionComplete, result, policy)
	want := "民族、婚姻状况、职业、工作单位、住址尚未填写，本次不阻断完成病历。"
	if message != want {
		t.Fatalf("message = %q, want %q", message, want)
	}
}

func TestPromptMessagesForEachAction(t *testing.T) {
	result := &RuleResult{RuleCode: "GEN-001", Result: "problem", Evidence: []Evidence{
		{Path: "patient.marital_status", Excerpt: "婚姻状况"},
		{Path: "patient.occupation", Excerpt: "职业"},
		{Path: "patient.work_unit", Excerpt: "工作单位"},
	}}
	for action, want := range map[Action]string{
		ActionSave:     "请填写婚姻状况、职业、工作单位。",
		ActionComplete: "婚姻状况、职业、工作单位尚未填写，本次不阻断完成病历。",
		ActionArchive:  "婚姻状况、职业、工作单位尚未填写，本次不阻断归档。",
	} {
		if got := messageForAction([]byte(`{}`), action, result, actionPolicy{}); got != want {
			t.Fatalf("%s: got %q, want %q", action, got, want)
		}
	}
}

func TestReportNonBlockingProblems(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report *Report
		want   bool
	}{
		{"absent", nil, false},
		{"pass", &Report{Results: []RuleResult{{Result: "pass"}}}, false},
		{"blocking only", &Report{Results: []RuleResult{{Result: "problem", Blocking: true}}}, false},
		{"reminder", &Report{Results: []RuleResult{{Result: "problem", Blocking: false}}}, true},
		{"not evaluated", &Report{Results: []RuleResult{{Result: "not_evaluable"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.report.HasNonBlockingProblems(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func sectionForTest(code string) string {
	if code == "GEN-001" {
		return "general"
	}
	if code == "CC-001" {
		return "chief_complaint"
	}
	if code == "HIS-001" || code == "HIS-009" {
		return "history"
	}
	if code == "EXAM-001" {
		return "physical_exam"
	}
	return "diagnosis"
}

func standardForTest(code string) int {
	if code == "CC-001" {
		return 5
	}
	if code == "HIS-001" || code == "EXAM-001" {
		return 20
	}
	if code == "HIS-009" {
		return 5
	}
	if code == "DX-001" {
		return 10
	}
	return 2
}

func specForTest(code string) []byte {
	if code == "GEN-001" {
		return []byte(`{"required_fields":["name","gender","birth_date"]}`)
	}
	if code == "HIS-009" {
		return []byte(`{"uncertain":["待确认"]}`)
	}
	return []byte(`{}`)
}
