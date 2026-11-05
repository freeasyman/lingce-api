package v210

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler/v200"
)

func rc11Compile(t *testing.T, in, mid []byte, version string) (map[string]any, []Diagnostic) {
	t.Helper()
	r, err := Compile(in, mid, version)
	if err != nil {
		t.Fatalf("%s compile: %v", version, err)
	}
	var card map[string]any
	if err = json.Unmarshal(r.Card, &card); err != nil {
		t.Fatal(err)
	}
	return card, r.Diagnostics
}

func rc11Decisions(card map[string]any) []map[string]any {
	raw, _ := card["recording_observations"].([]any)
	out := []map[string]any{}
	for _, item := range raw {
		out = append(out, item.(map[string]any))
	}
	return out
}

func rc11IntentFor(card map[string]any, target string) map[string]any {
	for _, obs := range rc11Decisions(card) {
		if obs["kind"] == "intent" && obs["target"] == target {
			return obs
		}
	}
	return nil
}

func rc11StaffClaimFor(card map[string]any, target string) map[string]any {
	for _, obs := range rc11Decisions(card) {
		if obs["kind"] == "staff_claim" && obs["target"] == target {
			return obs
		}
	}
	return nil
}

func rc11HasDiagnostic(diags []Diagnostic, path, code string) bool {
	for _, d := range diags {
		if d.Path == path && d.Code == code {
			return true
		}
	}
	return false
}

func rc11EmployeeDecision(t *testing.T) ([]byte, []byte) {
	t.Helper()
	in, mid := v210Fixture(t)
	var env map[string]any
	if err := json.Unmarshal(in, &env); err != nil {
		t.Fatal(err)
	}
	input := env["input"].(map[string]any)
	for _, raw := range input["transcript_sources"].([]any) {
		r := raw.(map[string]any)
		if r["id"] == "s3" {
			r["speaker"] = "未知"
			r["role_basis"] = "unknown"
		}
	}
	var m map[string]any
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	d := m["decision"].(map[string]any)
	d["state"] = "明确不考虑"
	d["target"] = "方案乙"
	d["source_refs"] = []any{"s3"}
	d["condition"] = nil
	d["need"] = map[string]any{"state": "无法判断", "summary": nil, "source_refs": []any{}, "support": nil}
	d["concerns"] = []any{}
	d["trust"] = []any{}
	m["additional_decisions"] = []any{}
	delete(m, "analysis_scope")
	// Re-hash after changing the input so the snapshot binding stays valid.
	id, err := v200.SnapshotID(must(t, env["input"]))
	if err != nil {
		t.Fatal(err)
	}
	env["input_snapshot_id"] = id
	m["input_snapshot_id"] = id
	return must(t, env), must(t, m)
}

// TestRC11EmployeeConclusionIsNotCustomerIntent is the regression for evaluation
// id 26: staff said the ICL option was "pass"; rc10 published that as the
// customer's "明确不考虑". rc11 must keep the information but not as kind=intent.
func TestRC11EmployeeConclusionIsNotCustomerIntent(t *testing.T) {
	in, mid := rc11EmployeeDecision(t)
	rc10, _ := rc11Compile(t, in, mid, RulesVersionRC10)
	if rc11IntentFor(rc10, "方案乙") == nil {
		t.Fatal("rc10 fixture no longer reproduces the employee-to-intent leak")
	}
	rc11, diags := rc11Compile(t, in, mid, RulesVersionRC11)
	if got := rc11IntentFor(rc11, "方案乙"); got != nil {
		t.Fatalf("rc11 still publishes employee conclusion as customer intent: %+v", got)
	}
	claim := rc11StaffClaimFor(rc11, "方案乙")
	if claim == nil {
		t.Fatal("rc11 dropped the staff conclusion instead of preserving it")
	}
	if claim["summary"] != rc11StaffClaimTemplate {
		t.Fatalf("staff claim summary is not the server template: %v", claim["summary"])
	}
	if !rc11HasDiagnostic(diags, "$.decision", "intent_not_customer_bound") {
		t.Fatalf("missing intent_not_customer_bound diagnostic: %+v", diags)
	}
	// The target/evaluation content must survive; only the wrong attribution goes.
	raw, _ := json.Marshal(rc11)
	if !bytes.Contains(raw, []byte("方案乙")) {
		t.Fatal("rc11 erased the scheme target")
	}
}

// TestRC11StrongRefusalNeedsCustomerWording covers the rule that "明确不考虑"
// is only a customer's direct, unconditional refusal.
func TestRC11StrongRefusalNeedsCustomerWording(t *testing.T) {
	in, mid := v210Fixture(t)
	var m map[string]any
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	d := m["decision"].(map[string]any)
	d["state"] = "明确不考虑"
	d["target"] = "方案甲"
	d["source_refs"] = []any{"s2"} // s2 is a cleaned customer turn
	d["condition"] = nil
	mid = must(t, m)

	rc11, _ := rc11Compile(t, in, mid, RulesVersionRC11)
	if got := rc11IntentFor(rc11, "方案甲"); got == nil || got["summary"] != "明确不考虑" {
		t.Fatalf("customer-spoken refusal was dropped: %+v", got)
	}
	intent := rc11["intent"].(map[string]any)
	if intent["state"] != "明确不考虑" {
		t.Fatalf("top-level intent lost the customer refusal: %v", intent["state"])
	}
}

// TestRC11UnknownSpeakerRefusalIsWithheld verifies the same refusal from an
// unknown speaker is not published as the customer's state at all.
func TestRC11UnknownSpeakerRefusalIsWithheld(t *testing.T) {
	in, mid := v210Fixture(t)
	var env map[string]any
	var m map[string]any
	if err := json.Unmarshal(in, &env); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	for _, raw := range env["input"].(map[string]any)["transcript_sources"].([]any) {
		r := raw.(map[string]any)
		if r["id"] == "s3" {
			r["speaker"] = "未知"
			r["role_basis"] = "unknown"
		}
	}
	d := m["decision"].(map[string]any)
	d["state"] = "明确不考虑"
	d["target"] = "方案甲"
	d["source_refs"] = []any{"s3"}
	d["condition"] = nil
	id, err := v200.SnapshotID(must(t, env["input"]))
	if err != nil {
		t.Fatal(err)
	}
	env["input_snapshot_id"] = id
	m["input_snapshot_id"] = id
	in, mid = must(t, env), must(t, m)

	rc11, diags := rc11Compile(t, in, mid, RulesVersionRC11)
	if got := rc11IntentFor(rc11, "方案甲"); got != nil {
		t.Fatalf("unknown speaker still became customer intent: %+v", got)
	}
	if intent := rc11["intent"].(map[string]any); intent["state"] != "无法判断" {
		t.Fatalf("top-level intent kept an unknown-speaker state: %v", intent["state"])
	}
	if rc11StaffClaimFor(rc11, "方案甲") == nil {
		t.Fatal("unknown-speaker conclusion was erased instead of kept as staff claim")
	}
	if !rc11HasDiagnostic(diags, "$.decision", "intent_not_customer_bound") {
		t.Fatal("missing intent_not_customer_bound diagnostic")
	}
	claims, _ := json.Marshal(rc11)
	if bytes.Contains(claims, []byte("明确不考虑")) {
		t.Fatal("rc11 card still contains the unknown-speaker strong refusal")
	}
}

// TestRC11BoundsLongMixedStatement is the regression for evaluation id 18: a
// 3,963-character ASR segment was published whole as a concern. The excerpt must
// keep the cited customer sentence, not the mixed-role remainder.
func TestRC11BoundsLongMixedStatement(t *testing.T) {
	in, mid := v210Fixture(t)
	var env map[string]any
	var m map[string]any
	if err := json.Unmarshal(in, &env); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("员工在说明流程和等候安排。", 20) + "客户说想先做检查再决定。" + strings.Repeat("后面还有多人对话和无关流程。", 20)
	spanText := "客户说想先做检查再决定。"
	start := len([]rune(strings.Repeat("员工在说明流程和等候安排。", 20)))
	end := start + len([]rune(spanText))
	for _, raw := range env["input"].(map[string]any)["transcript_sources"].([]any) {
		r := raw.(map[string]any)
		if r["id"] == "s2" {
			r["text"] = long
		}
	}
	d := m["decision"].(map[string]any)
	d["need"] = map[string]any{
		"state":       "明确",
		"summary":     "客户想先做检查",
		"source_refs": []any{"s2"},
		"support": map[string]any{
			"statement_type": "customer_need",
			"confidence":     "medium",
			"spans":          []any{map[string]any{"source_ref": "s2", "start": start, "end": end}},
		},
	}
	id, err := v200.SnapshotID(must(t, env["input"]))
	if err != nil {
		t.Fatal(err)
	}
	env["input_snapshot_id"] = id
	m["input_snapshot_id"] = id
	in, mid = must(t, env), must(t, m)

	rc10, _ := rc11Compile(t, in, mid, RulesVersionRC10)
	rc11, diags := rc11Compile(t, in, mid, RulesVersionRC11)
	needOf := func(card map[string]any) string {
		for _, obs := range rc11Decisions(card) {
			if obs["kind"] == "need" {
				return obs["summary"].(string)
			}
		}
		return ""
	}
	need10 := needOf(rc10)
	need11 := needOf(rc11)
	if len([]rune(need10)) != len([]rune(long)) {
		t.Fatalf("rc10 fixture no longer publishes the whole utterance: %d", len([]rune(need10)))
	}
	if len([]rune(need11)) > rc11MaxStatementRunes+2 {
		t.Fatalf("rc11 statement not bounded: %d", len([]rune(need11)))
	}
	if !strings.Contains(need11, spanText) {
		t.Fatalf("rc11 excerpt lost the cited customer sentence: %q", need11)
	}
	if strings.Contains(need11, "无关流程") {
		t.Fatalf("rc11 excerpt kept unrelated mixed-role text: %q", need11)
	}
	if !rc11HasDiagnostic(diags, "$.decision.need", "statement_summary_truncated") {
		t.Fatalf("missing truncation diagnostic: %+v", diags)
	}
}

// TestRC11NonBusinessScopeSurvivesServerSummary is the regression for id 17.
func TestRC11NonBusinessScopeSurvivesServerSummary(t *testing.T) {
	in, mid := v210Fixture(t)
	var m map[string]any
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	m["analysis_scope"] = map[string]any{"kind": "non_business", "source_refs": []any{"s1"}}
	mid = must(t, m)

	rc11, _ := rc11Compile(t, in, mid, RulesVersionRC11)
	scope := rc11["analysis_scope"].(map[string]any)
	if scope["kind"] != "non_business" {
		t.Fatalf("non-business scope lost: %+v", scope)
	}
	if scope["label"] != rc11NonBusinessLabel {
		t.Fatalf("scope label is not server template: %v", scope["label"])
	}
	conclusion := rc11["conclusion"].(map[string]any)
	if !strings.Contains(conclusion["current_state"].(string), "非业务") {
		t.Fatalf("server summary hid the non-business label: %v", conclusion["current_state"])
	}
	obs := rc11Decisions(rc11)
	if len(obs) == 0 || obs[0]["kind"] != "non_business" {
		t.Fatalf("non-business label missing from recording observations: %+v", obs)
	}
	if len(scope["evidence_refs"].([]any)) == 0 {
		t.Fatal("non-business scope has no evidence")
	}
}

// TestRC11WithoutScopeStaysUnclear keeps rc10 responses compilable under rc11.
func TestRC11WithoutScopeStaysUnclear(t *testing.T) {
	in, mid := v210Fixture(t)
	rc11, _ := rc11Compile(t, in, mid, RulesVersionRC11)
	scope := rc11["analysis_scope"].(map[string]any)
	if scope["kind"] != "unclear" {
		t.Fatalf("missing scope was not unclear: %+v", scope)
	}
	if _, ok := rc11["analysis_scope"]; !ok {
		t.Fatal("rc11 card lacks analysis_scope")
	}
}

// TestRC11GatingLeavesRC10ByteStable proves all new behavior is gated: without
// the rc11-only fields the card equals rc10's except rules_version and the new
// analysis_scope.
func TestRC11GatingLeavesRC10ByteStable(t *testing.T) {
	in, mid := v210Fixture(t)
	rc10, _ := rc11Compile(t, in, mid, RulesVersionRC10)
	rc11, _ := rc11Compile(t, in, mid, RulesVersionRC11)
	delete(rc11, "analysis_scope")
	rc10["rules_version"] = RulesVersionRC11
	a, _ := json.Marshal(rc10)
	b, _ := json.Marshal(rc11)
	if !bytes.Equal(a, b) {
		t.Fatalf("rc11 changed rc10 behavior beyond analysis_scope/rules_version:\nrc10=%s\nrc11=%s", a, b)
	}
}
