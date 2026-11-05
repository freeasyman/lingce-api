package v210

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler/v200"
)

func rc12Obs(card map[string]any, kind string) []map[string]any {
	out := []map[string]any{}
	raw, _ := card["recording_observations"].([]any)
	for _, item := range raw {
		obs := item.(map[string]any)
		if obs["kind"] == kind {
			out = append(out, obs)
		}
	}
	return out
}

func rc12Summary(obs map[string]any) string {
	text, _ := obs["summary"].(string)
	return text
}

func rc12Hits(diags []Diagnostic, path, code string) bool {
	for _, d := range diags {
		if d.Path == path && d.Code == code {
			return true
		}
	}
	return false
}

// rc12Rehash rewrites the snapshot id after the fixture was changed.
func rc12Rehash(t *testing.T, env, m map[string]any) ([]byte, []byte) {
	t.Helper()
	id, err := v200.SnapshotID(must(t, env["input"]))
	if err != nil {
		t.Fatal(err)
	}
	env["input_snapshot_id"] = id
	m["input_snapshot_id"] = id
	return must(t, env), must(t, m)
}

// rc12TwoQuoteConcern builds one concern supported by two long customer quotes.
// rc11 bounds each excerpt separately, so the published statement exceeds the
// 240-rune display cap (evaluation id 19: 257 runes).
func rc12TwoQuoteConcern(t *testing.T, customerBound bool) ([]byte, []byte) {
	t.Helper()
	in, mid := v210Fixture(t)
	var env, m map[string]any
	if err := json.Unmarshal(in, &env); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	first := strings.Repeat("我先扛扛，周末再歇着，", 12) + "。"
	second := strings.Repeat("我还得上班接孩子，", 20) + "。"
	for _, raw := range env["input"].(map[string]any)["transcript_sources"].([]any) {
		r := raw.(map[string]any)
		switch r["id"] {
		case "s2":
			r["text"] = first
		case "s4":
			r["text"] = second
		}
	}
	if customerBound {
		ctx := env["input"].(map[string]any)["context"].(map[string]any)
		ctx["target_binding"] = "single_customer"
		ctx["identity_confirmed"] = true
		ctx["target_customer_id"] = 1
		ctx["binding_record_id"] = "bind-1"
		for _, raw := range env["input"].(map[string]any)["transcript_sources"].([]any) {
			r := raw.(map[string]any)
			if r["speaker"] == "客户" {
				r["subject_binding"] = "target_customer"
				r["speaker_id"] = "cust-1"
				r["binding_record_id"] = "bind-1"
			}
		}
	}
	d := m["decision"].(map[string]any)
	d["concerns"] = []any{map[string]any{
		"type":       "时间与流程",
		"type_label": nil,
		"summary":    "客户需要上班和接送孩子",
		"is_primary": true,
		"source_refs": []any{"s2", "s4"},
		"support": map[string]any{
			"statement_type": "customer_feeling",
			"confidence":     "medium",
			"spans": []any{
				map[string]any{"source_ref": "s2", "start": 0, "end": len([]rune(first))},
				map[string]any{"source_ref": "s4", "start": 0, "end": len([]rune(second))},
			},
		},
	}}
	d["need"] = map[string]any{"state": "无法判断", "summary": nil, "source_refs": []any{}, "support": nil}
	return rc12Rehash(t, env, m)
}

// TestRC12CapsTotalStatement is the regression for id 19: two verified quotes were
// concatenated into a 257-rune summary even though rc11 capped each excerpt.
func TestRC12CapsTotalStatement(t *testing.T) {
	in, mid := rc12TwoQuoteConcern(t, false)
	rc11, _ := rc11Compile(t, in, mid, RulesVersionRC11)
	rc12, diags := rc11Compile(t, in, mid, RulesVersionRC12)
	firstOf := func(card map[string]any) string {
		obs := rc12Obs(card, "concern")
		if len(obs) == 0 {
			return ""
		}
		return rc12Summary(obs[0])
	}
	c11, c12 := firstOf(rc11), firstOf(rc12)
	if len([]rune(c11)) <= rc12MaxStatementRunes {
		t.Fatalf("rc11 fixture no longer exceeds the display cap: %d", len([]rune(c11)))
	}
	if len([]rune(c12)) > rc12MaxStatementRunes+2 {
		t.Fatalf("rc12 statement still exceeds the display cap: %d", len([]rune(c12)))
	}
	if !strings.Contains(c12, "我先扛扛") {
		t.Fatalf("rc12 dropped the whole statement instead of the second excerpt: %q", c12)
	}
	if strings.Contains(c12, "我还得上班接孩子") {
		t.Fatalf("rc12 kept the over-budget second excerpt: %q", c12)
	}
	if !rc12Hits(diags, "$.decision.concerns[0]", "statement_summary_total_truncated") {
		t.Fatalf("missing total truncation diagnostic: %+v", diags)
	}
	// The omitted source stays cited so a reviewer can still open it.
	refs, _ := rc12Obs(rc12, "concern")[0]["evidence_refs"].([]any)
	if len(refs) != 2 {
		t.Fatalf("rc12 dropped the second evidence ref: %+v", refs)
	}
}

// TestRC12DropsDuplicateExcerpt is the other half of id 19: the same customer
// quote was published three times across schemes.
func TestRC12DropsDuplicateExcerpt(t *testing.T) {
	in, mid := rc12TwoQuoteConcern(t, true)
	var env, m map[string]any
	if err := json.Unmarshal(in, &env); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	primary := m["decision"].(map[string]any)
	extra := map[string]any{}
	for k, v := range primary {
		extra[k] = v
	}
	extra["target"] = "方案乙"
	extra["concerns"] = primary["concerns"]
	m["additional_decisions"] = []any{extra}
	in, mid = rc12Rehash(t, env, m)

	rc11, _ := rc11Compile(t, in, mid, RulesVersionRC11)
	rc12, diags := rc11Compile(t, in, mid, RulesVersionRC12)
	if len(rc12Obs(rc11, "concern")) != 2 {
		t.Fatalf("rc11 fixture no longer republishes the same excerpt: %d", len(rc12Obs(rc11, "concern")))
	}
	obs := rc12Obs(rc12, "concern")
	if len(obs) != 1 {
		t.Fatalf("rc12 did not de-duplicate the repeated quote: %d", len(obs))
	}
	if !rc12Hits(diags, "$.additional_decisions[0].concerns[0]", "duplicate_statement_dropped") {
		t.Fatalf("missing duplicate diagnostic: %+v", diags)
	}
	excerpt := rc12Summary(obs[0])
	repeats := 0
	for _, other := range rc12Obs(rc12, "concern") {
		if rc12Summary(other) == excerpt {
			repeats++
		}
	}
	if repeats != 1 {
		t.Fatalf("rc12 still repeats the same excerpt %d times", repeats)
	}
}

// TestRC12PublishesLocalObservations covers the structured channel for the four
// defects: a spoken window keeps its original wording in a structured timing
// slot, a willingness keeps the customer's own words, and an examination action
// keeps its verbatim sentence instead of only appearing in an evidence quote.
func TestRC12PublishesLocalObservations(t *testing.T) {
	in, mid := v210Fixture(t)
	var env, m map[string]any
	if err := json.Unmarshal(in, &env); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	for _, raw := range env["input"].(map[string]any)["transcript_sources"].([]any) {
		r := raw.(map[string]any)
		switch r["id"] {
		case "s2":
			r["text"] = "行，那你就这两周之内确认，不怀孕就约8月1号手术。"
		case "s3":
			r["text"] = "来，腿伸直，我按这个位置疼不疼？"
		case "s4":
			r["text"] = "疼，先不做了，我再考虑考虑。"
		}
	}
	m["local_observations"] = []any{
		map[string]any{"kind": "timing", "summary": "这两周之内确认", "source_refs": []any{"s2"}, "timing_kind": "relative_window"},
		map[string]any{"kind": "timing", "summary": "约8月1号手术", "source_refs": []any{"s2"}, "timing_kind": "absolute_date"},
		map[string]any{"kind": "exam", "summary": "腿伸直，我按这个位置疼不疼？", "source_refs": []any{"s3"}},
		map[string]any{"kind": "willingness", "summary": "先不做了", "source_refs": []any{"s4"}},
	}
	in, mid = rc12Rehash(t, env, m)

	rc12, diags := rc11Compile(t, in, mid, RulesVersionRC12)
	timings := rc12Obs(rc12, "timing")
	if len(timings) != 2 {
		t.Fatalf("timing observations: %+v", timings)
	}
	seen := map[string]map[string]any{}
	for _, obs := range timings {
		timing, ok := obs["timing"].(map[string]any)
		if !ok {
			t.Fatalf("timing observation has no structured timing object: %+v", obs)
		}
		if timing["date"] != nil {
			t.Fatalf("server must leave the date null: %+v", timing)
		}
		seen[rc12Summary(obs)] = timing
	}
	if seen["这两周之内确认"]["kind"] != "relative_window" {
		t.Fatalf("relative window lost its kind: %+v", seen["这两周之内确认"])
	}
	if seen["约8月1号手术"]["kind"] != "absolute_date" {
		t.Fatalf("absolute date lost its kind: %+v", seen["约8月1号手术"])
	}
	exam := rc12Obs(rc12, "exam")
	if len(exam) != 1 || !strings.Contains(rc12Summary(exam[0]), "我按这个位置疼不疼") {
		t.Fatalf("exam observation lost the verbatim sentence: %+v", exam)
	}
	willing := rc12Obs(rc12, "willingness")
	if len(willing) != 1 || rc12Summary(willing[0]) != "先不做了" {
		t.Fatalf("willingness lost the original words: %+v", willing)
	}
	for _, d := range diags {
		if strings.HasPrefix(d.Path, "$.local_observations") {
			t.Fatalf("verbatim local observation produced a diagnostic: %+v", d)
		}
	}
}

// TestRC12ReplacesParaphrasedLocalObservation keeps the display verbatim: a model
// paraphrase is replaced by the source excerpt instead of being published.
func TestRC12ReplacesParaphrasedLocalObservation(t *testing.T) {
	in, mid := v210Fixture(t)
	var m map[string]any
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	m["local_observations"] = []any{
		map[string]any{"kind": "willingness", "summary": "客户想尽快安排检查", "source_refs": []any{"s2"}},
	}
	rc12, diags := rc11Compile(t, in, must(t, m), RulesVersionRC12)
	obs := rc12Obs(rc12, "willingness")
	if len(obs) != 1 {
		t.Fatalf("paraphrased local observation was dropped instead of corrected: %+v", obs)
	}
	text := rc12Summary(obs[0])
	if strings.Contains(text, "尽快安排") {
		t.Fatalf("rc12 published the paraphrase: %q", text)
	}
	source := "我想了解视力矫正，之前交了定金，后续还是要等家里人商量好。"
	if !strings.Contains(source, strings.Trim(text, "…")) {
		t.Fatalf("rc12 fallback is not a source excerpt: %q", text)
	}
	if !rc12Hits(diags, "$.local_observations[0]", "local_observation_not_verbatim") {
		t.Fatalf("missing paraphrase diagnostic: %+v", diags)
	}
}

// TestRC12LocalObservationRequiresSource drops a fragment whose citation does not
// resolve; an observation must never be published without a source.
func TestRC12LocalObservationRequiresSource(t *testing.T) {
	in, mid := v210Fixture(t)
	var m map[string]any
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	m["local_observations"] = []any{
		map[string]any{"kind": "timing", "summary": "这两周之内", "source_refs": []any{"s99"}, "timing_kind": "relative_window"},
	}
	rc12, diags := rc11Compile(t, in, must(t, m), RulesVersionRC12)
	if len(rc12Obs(rc12, "timing")) != 0 {
		t.Fatalf("unresolved citation was published: %+v", rc12Obs(rc12, "timing"))
	}
	if !rc12Hits(diags, "$.local_observations[0].source_refs[0]", "unknown_source") {
		t.Fatalf("missing unknown_source diagnostic: %+v", diags)
	}
}

// TestRC12GatingLeavesRC11ByteStable proves the new behavior is gated: without the
// rc12-only channel the card equals rc11 apart from rules_version.
func TestRC12GatingLeavesRC11ByteStable(t *testing.T) {
	in, mid := v210Fixture(t)
	rc11, _ := rc11Compile(t, in, mid, RulesVersionRC11)
	rc12, _ := rc11Compile(t, in, mid, RulesVersionRC12)
	rc11["rules_version"] = RulesVersionRC12
	a, _ := json.Marshal(rc11)
	b, _ := json.Marshal(rc12)
	if !bytes.Equal(a, b) {
		t.Fatalf("rc12 changed rc11 behavior beyond rules_version:\nrc11=%s\nrc12=%s", a, b)
	}
}

// TestRC12ContractsAreAdditive keeps earlier schema bytes frozen and makes the new
// local_observations channel optional so rc11 saved responses stay recompilable.
func TestRC12ContractsAreAdditive(t *testing.T) {
	if !IsRulesVersion(RulesVersionRC12) {
		t.Fatal("rc12 rules version is not executable")
	}
	i11, m11, f11 := ContractsForVersion(RulesVersionRC11)
	i12, m12, f12 := ContractsForVersion(RulesVersionRC12)
	if bytes.Equal(i11, i12) == false {
		t.Fatal("rc12 changed the frozen input schema")
	}
	if bytes.Equal(m11, m12) {
		t.Fatal("rc12 did not add its own intermediate schema")
	}
	if bytes.Equal(f11, f12) {
		t.Fatal("rc12 reused the rc11 final card schema")
	}
	var mid map[string]any
	if err := json.Unmarshal(m12, &mid); err != nil {
		t.Fatal(err)
	}
	if _, ok := mid["properties"].(map[string]any)["local_observations"]; !ok {
		t.Fatal("rc12 intermediate schema lacks local_observations")
	}
	for _, raw := range mid["required"].([]any) {
		if raw == "local_observations" {
			t.Fatal("local_observations must stay optional so rc11 responses recompile")
		}
	}
	var card map[string]any
	if err := json.Unmarshal(f12, &card); err != nil {
		t.Fatal(err)
	}
	obs := card["properties"].(map[string]any)["recording_observations"].(map[string]any)["items"].(map[string]any)
	kinds := obs["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]any)
	seen := map[string]bool{}
	for _, raw := range kinds {
		seen[raw.(string)] = true
	}
	for _, want := range []string{"timing", "willingness", "exam", "staff_claim", "non_business"} {
		if !seen[want] {
			t.Fatalf("rc12 observation kinds missing %q: %+v", want, kinds)
		}
	}
	if _, ok := obs["properties"].(map[string]any)["timing"]; !ok {
		t.Fatal("rc12 card schema lacks the structured timing slot")
	}
}

// TestRC11IntermediateRejectsLocalObservations keeps rc11 immutable: a response
// that uses the rc12 channel cannot be recompiled as rc11.
func TestRC11IntermediateRejectsLocalObservations(t *testing.T) {
	in, mid := v210Fixture(t)
	var m map[string]any
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	m["local_observations"] = []any{map[string]any{"kind": "exam", "summary": "腿伸直", "source_refs": []any{"s3"}}}
	if _, err := Compile(in, must(t, m), RulesVersionRC11); err == nil {
		t.Fatal("rc11 accepted the rc12-only field")
	}
}
