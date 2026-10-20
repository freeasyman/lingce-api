package dealexpert

import (
	"strings"
	"testing"
)

const validResult = `{
  "conclusion": {"current_state": "有明确意向但未锁定下一步"},
  "decision": {"state": "有意向但未决定", "concern_type": "家属或共同决策人"},
  "product_findings": [],
  "primary_action": {"goal": "确认家属顾虑"},
  "confidence": "high",
  "unknowns": []
}`

func TestParseAnalysisResultAcceptsValidJSON(t *testing.T) {
	result, err := parseAnalysisResult(validResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["confidence"] != "high" {
		t.Fatalf("confidence = %v", result["confidence"])
	}
}

func TestParseAnalysisResultStripsCodeFence(t *testing.T) {
	fenced := "```json\n" + validResult + "\n```"
	if _, err := parseAnalysisResult(fenced); err != nil {
		t.Fatalf("fenced JSON should parse: %v", err)
	}
}

func TestParseAnalysisResultRejectsMissingKeys(t *testing.T) {
	_, err := parseAnalysisResult(`{"conclusion": {}, "decision": {}}`)
	if err == nil || !strings.Contains(err.Error(), "product_findings") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestParseAnalysisResultRejectsWrongTypes(t *testing.T) {
	_, err := parseAnalysisResult(`{
	  "conclusion": [],
	  "decision": {},
	  "product_findings": [],
	  "primary_action": {},
	  "confidence": "high"
	}`)
	if err == nil || !strings.Contains(err.Error(), "conclusion must be an object") {
		t.Fatalf("expected conclusion type error, got %v", err)
	}
}

func TestParseAnalysisResultRejectsInvalidJSON(t *testing.T) {
	if _, err := parseAnalysisResult("not json"); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestRenderAnalysisPromptReplacesPlaceholders(t *testing.T) {
	template := "客户：{{customer_name}}\n产品：{{products}}\n结果：{{outcome_facts}}\n转写：{{transcript}}"
	rendered := renderAnalysisPrompt(template, analysisPromptData{
		Transcript:     "客户说回去商量",
		CustomerName:   "王女士",
		ProductCatalog: []map[string]any{{"id": 3, "name": "全飞秒Pro"}},
		OutcomeFacts:   []map[string]any{{"type": "预约", "amount": 0}},
	})
	for _, want := range []string{"王女士", `"全飞秒Pro"`, `"预约"`, "客户说回去商量"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered prompt missing %q: %s", want, rendered)
		}
	}
	if strings.Contains(rendered, "{{") {
		t.Fatalf("unreplaced placeholder remains: %s", rendered)
	}
}

func TestTruncateRunesKeepsShortText(t *testing.T) {
	if got := truncateRunes("短文本", 10); got != "短文本" {
		t.Fatalf("short text changed: %q", got)
	}
	long := strings.Repeat("字", 30)
	got := truncateRunes(long, 10)
	if !strings.HasPrefix(got, strings.Repeat("字", 10)) || !strings.Contains(got, "已截断") {
		t.Fatalf("truncate result unexpected: %q", got)
	}
}
