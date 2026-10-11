package complianceguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func validCandidate() map[string]any {
	return map[string]any{"theme_key": "effect_conditions", "rule_code": "r1", "source_rule_codes": []any{"r1"}, "question": "该效果是否存在无条件承诺？", "risk_hypothesis": "可能影响患者判断。", "primary_risk_type": "patient_decision", "related_risk_types": []any{"legal_evidence"}, "check_basis": []any{"沟通标准"}, "suggested_actions": []any{"与当事人沟通"}, "priority": "important", "is_normal_communication": false, "review_recommended": true, "evidence": []any{map[string]any{"quote": "保证治好", "speaker": "医生", "segment_index": nil, "start_second": nil, "end_second": nil}}}
}
func TestCandidateValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing boolean", func(m map[string]any) { delete(m, "review_recommended") }},
		{"null boolean", func(m map[string]any) { m["review_recommended"] = nil }},
		{"string boolean", func(m map[string]any) { m["is_normal_communication"] = "false" }},
		{"normal communication", func(m map[string]any) { m["is_normal_communication"] = true }},
		{"not recommended", func(m map[string]any) { m["review_recommended"] = false }},
		{"unknown risk", func(m map[string]any) { m["related_risk_types"] = []any{"unknown"} }},
		{"null risks", func(m map[string]any) { m["related_risk_types"] = nil }},
		{"numeric basis", func(m map[string]any) { m["check_basis"] = []any{1} }},
		{"object action", func(m map[string]any) { m["suggested_actions"] = []any{map[string]any{"a": "b"}} }},
		{"null action", func(m map[string]any) { m["suggested_actions"] = []any{nil} }},
		{"empty basis", func(m map[string]any) { m["check_basis"] = []any{" "} }},
		{"illegal evidence alongside valid", func(m map[string]any) { m["evidence"] = append(m["evidence"].([]any), nil) }},
		{"invented quote", func(m map[string]any) { m["evidence"].([]any)[0].(map[string]any)["quote"] = "保证恢复正常" }},
		{"unknown primary rule", func(m map[string]any) { m["rule_code"] = "other" }},
		{"unknown source rule", func(m map[string]any) { m["source_rule_codes"] = []any{"r1", "other"} }},
		{"missing primary in source", func(m map[string]any) { m["source_rule_codes"] = []any{"r2"} }},
		{"invalid time", func(m map[string]any) { m["evidence"].([]any)[0].(map[string]any)["start_second"] = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validCandidate()
			tt.mutate(m)
			raw, _ := json.Marshal(map[string]any{"findings": []any{m}})
			if _, err := decodeCommunicationFindings(string(raw), "医生：保证治好", []string{"r1", "r2"}); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	raw, _ := json.Marshal(map[string]any{"findings": []any{validCandidate()}})
	if _, err := decodeCommunicationFindings(string(raw), "医生：保证治好", []string{"r1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeCommunicationFindings(`{"findings":[]}`, "正常告知", []string{"r1"}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"findings":null}`, `{"findings":[`, "```json\n{\"findings\":[]}\n```"} {
		if _, err := decodeCommunicationFindings(raw, "", nil); err == nil {
			t.Fatal("expected strict JSON rejection")
		}
	}
}

// Replay results are external artifacts, never embedded responses masquerading
// as model judgment tests. This opt-in test validates the real model outputs.
func TestGatewayReplayContract(t *testing.T) {
	dir := os.Getenv("COMPLIANCE_REPLAY_DIR")
	if dir == "" {
		t.Skip("set COMPLIANCE_REPLAY_DIR to validate actual gateway responses")
	}
	var summary struct {
		Rules   []string `json:"rules"`
		Results []struct {
			Sample string `json:"sample"`
			Input  string `json:"input"`
		} `json:"results"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &summary); err != nil {
		t.Fatal(err)
	}
	for _, sample := range summary.Results {
		t.Run(sample.Sample, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(dir, sample.Sample+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				Content string `json:"content"`
			}
			if err = json.Unmarshal(b, &response); err != nil {
				t.Fatal(err)
			}
			if _, err = decodeCommunicationFindings(response.Content, sample.Input, summary.Rules); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRuntimeParserKeepsConsultantLegacyCompatibility(t *testing.T) {
	legacy := `{"findings":[{"rule_code":"legacy.rule","summary":"旧格式","evidence":[{"quote":"原话"}]}]}`
	if _, err := parseFindingsForRuntime(legacy, "原话", []string{"legacy.rule"}, "legacy"); err != nil {
		t.Fatalf("consultant legacy result rejected: %v", err)
	}
	if _, err := parseFindingsForRuntime(legacy, "原话", []string{"legacy.rule"}, "risk_candidate_v1"); err == nil {
		t.Fatal("doctor legacy result accepted")
	}
}

func TestDetectCommunicationOutputContract(t *testing.T) {
	legacy := []byte(`{"type":"object","properties":{"findings":{"type":"array","items":{"required":["rule_code"]}}}}`)
	modern := []byte(`{"type":"object","properties":{"findings":{"type":"array","items":{"required":["rule_code","source_rule_codes"]}}}}`)
	if got, err := detectCommunicationOutputContract(legacy); err != nil || got != "legacy" {
		t.Fatalf("legacy contract = %q, err=%v", got, err)
	}
	if got, err := detectCommunicationOutputContract(modern); err != nil || got != "risk_candidate_v1" {
		t.Fatalf("modern contract = %q, err=%v", got, err)
	}
	if _, err := detectCommunicationOutputContract([]byte(`{"type":"object"}`)); err == nil {
		t.Fatal("expected malformed findings schema rejection")
	}
}

func TestSelectCommunicationOutputSchema(t *testing.T) {
	global := []byte(`{"global":true}`)
	custom := []byte(`{"custom":true}`)
	if got := selectCommunicationOutputSchema(global, custom); string(got) != string(custom) {
		t.Fatalf("custom schema did not override global: %s", got)
	}
	if got := selectCommunicationOutputSchema(global, nil); string(got) != string(global) {
		t.Fatalf("global schema not retained: %s", got)
	}
}
