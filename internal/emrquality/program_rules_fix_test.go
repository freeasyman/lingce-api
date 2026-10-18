package emrquality

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func programRule(code string, standard int, spec string) ruleDefinition {
	return ruleDefinition{
		Code:           code,
		SectionCode:    "general",
		Name:           code,
		StandardPoints: standard,
		ExecutorKind:   "program",
		EvaluationSpec: []byte(spec),
	}
}

func TestHIS009AcceptsNegativeAndRejectsUnasked(t *testing.T) {
	rule := programRule("HIS-009", 5, `{"accepted_negative":["无过敏史","否认药物过敏史","否认过敏史"],"uncertain":["不详"]}`)
	cases := []struct {
		text string
		want string
	}{
		{"否认药物过敏史", "pass"},
		{"不详", "problem"},
		{"未询问", "problem"},
		{"", "problem"},
	}
	for _, tc := range cases {
		got, err := evaluateRule(Context{Content: map[string]any{"allergy_history": tc.text}}, rule)
		if err != nil {
			t.Fatalf("evaluate HIS-009: %v", err)
		}
		if got.Result != tc.want {
			t.Fatalf("text %q: want %s, got %s", tc.text, tc.want, got.Result)
		}
	}
}

func TestGEN002SupportsCommonDateFormat(t *testing.T) {
	rule := programRule("GEN-002", 1, `{}`)
	started := time.Date(2026, 10, 18, 9, 0, 0, 0, time.Local)
	for _, birth := range []string{"2030-1-1", "2030/01/01", "2030.1.2"} {
		got, err := evaluateRule(Context{Patient: map[string]any{"birth_date": birth}, StartedAt: started}, rule)
		if err != nil {
			t.Fatalf("evaluate GEN-002: %v", err)
		}
		if got.Result != "problem" {
			t.Fatalf("birth %q should be problem, got %s", birth, got.Result)
		}
	}
	got, _ := evaluateRule(Context{Patient: map[string]any{"birth_date": "1990-1-1"}, StartedAt: started}, rule)
	if got.Result != "pass" {
		t.Fatalf("past birth should pass, got %s", got.Result)
	}
}

func TestGEN003OnlyFlagsYearOnly(t *testing.T) {
	rule := programRule("GEN-003", 1, `{}`)
	for _, birth := range []string{"1980", "1980年"} {
		got, _ := evaluateRule(Context{Patient: map[string]any{"birth_date": birth}}, rule)
		if got.Result != "problem" {
			t.Fatalf("birth %q should be problem, got %s", birth, got.Result)
		}
	}
	for _, birth := range []string{"1980-01", "1980/01", "1980.01"} {
		got, _ := evaluateRule(Context{Patient: map[string]any{"birth_date": birth}}, rule)
		if got.Result != "pass" {
			t.Fatalf("birth %q should pass, got %s", birth, got.Result)
		}
	}
}

func TestGEN001DeductionPerMissingField(t *testing.T) {
	rule := programRule("GEN-001", 2, `{"required_fields":["name","gender","birth_date"]}`)
	got, err := evaluateRule(Context{Patient: map[string]any{}}, rule)
	if err != nil {
		t.Fatalf("evaluate GEN-001: %v", err)
	}
	if got.Result != "problem" {
		t.Fatalf("want problem, got %s", got.Result)
	}
	if got.Deduction != 6 {
		t.Fatalf("want deduction 6 (3 fields * 2), got %d", got.Deduction)
	}
}

func TestSemanticBatchMalformedRequiresEveryRule(t *testing.T) {
	requested := map[string]bool{"CC-001": true, "CC-002": true}
	partial := &semanticResponse{Results: []semanticFinding{{RuleCode: "CC-001", Result: "pass"}}}
	reason := semanticBatchMalformed(partial, requested)
	if !strings.Contains(reason, "CC-002") {
		t.Fatalf("missing rule should be reported, got %q", reason)
	}
	full := &semanticResponse{Results: []semanticFinding{
		{RuleCode: "CC-001", Result: "pass"},
		{RuleCode: "CC-002", Result: "pass"},
	}}
	if reason := semanticBatchMalformed(full, requested); reason != "" {
		t.Fatalf("complete response should be accepted, got %q", reason)
	}
	dup := &semanticResponse{Results: []semanticFinding{
		{RuleCode: "CC-001", Result: "pass"},
		{RuleCode: "CC-001", Result: "pass"},
		{RuleCode: "CC-002", Result: "pass"},
	}}
	if reason := semanticBatchMalformed(dup, requested); !strings.Contains(reason, "重复") {
		t.Fatalf("duplicate rule should be reported, got %q", reason)
	}
}

func TestSemanticResponseUnmarshalStringEvidenceStillWorks(t *testing.T) {
	var response semanticResponse
	raw := `{"check_completed":true,"checked_rule_codes":["CC-001"],"results":[{"rule_code":"CC-001","result":"problem","evidence":"[{\"source\":\"record.content\",\"path\":\"chief_complaint\",\"excerpt\":\"头痛\"}]","reason":"x"}]}`
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(response.Results[0].Evidence) != 1 {
		t.Fatalf("want 1 evidence, got %d", len(response.Results[0].Evidence))
	}
}
