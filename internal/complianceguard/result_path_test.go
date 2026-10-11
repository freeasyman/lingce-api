package complianceguard

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBusinessParserAndPersistenceRejectInvalidResults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"forged quote", func(m map[string]any) { m["evidence"].([]any)[0].(map[string]any)["quote"] = "不存在的原话" }},
		{"unknown rule", func(m map[string]any) { m["rule_code"] = "unknown" }},
		{"missing action", func(m map[string]any) { delete(m, "suggested_actions") }},
		{"wrong boolean", func(m map[string]any) { m["review_recommended"] = "true" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := validCandidate()
			tc.mutate(item)
			raw, _ := json.Marshal(map[string]any{"findings": []any{item}})
			runtime := &communicationRuntime{OutputContract: "risk_candidate_v1", Request: CommunicationCheckRequest{CleanedTranscript: "医生：保证治好"}, RulesJSON: `[{"rule_code":"r1"}]`}
			if _, err := parseFindings(string(raw), runtime.Request.CleanedTranscript, runtimeRules(runtime)); err == nil {
				t.Fatal("business parser accepted invalid result")
			}
			// No database configured: reaching Begin would panic. The invalid result
			// must be rejected before any database operation, even if parser bypassed.
			err := (&Service{}).completeCommunicationRun(context.Background(), &CommunicationCheckResponse{Findings: []any{item}}, runtime, "fingerprint", "execution")
			if err == nil || !strings.Contains(err.Error(), "reject unverified") {
				t.Fatalf("invalid result reached persistence: %v", err)
			}
		})
	}
	item := validCandidate()
	raw, _ := json.Marshal(map[string]any{"findings": []any{item}})
	if _, err := parseFindings(string(raw), "医生：保证治好", []string{"r1"}); err != nil {
		t.Fatal(err)
	}
}
