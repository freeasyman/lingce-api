package v210

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler/v200"
)

// TestRC12P1AcceptsOtherLocalKind is the regression for real batch 09 / id 36:
// rc12's final card enum did not include the kind=other that its own intermediate
// schema allowed, so the compiler produced an invalid card and the record failed
// at compile_output. rc12.1 must publish it.
func TestRC12P1AcceptsOtherLocalKind(t *testing.T) {
	in, mid := v210Fixture(t)
	var env, m map[string]any
	if err := json.Unmarshal(in, &env); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	m["local_observations"] = []any{
		map[string]any{"kind": "other", "summary": "后续还是要等家里人商量好", "source_refs": []any{"s2"}},
	}
	id, err := v200.SnapshotID(must(t, env["input"]))
	if err != nil {
		t.Fatal(err)
	}
	env["input_snapshot_id"] = id
	m["input_snapshot_id"] = id

	if _, err := Compile(must(t, env), must(t, m), RulesVersionRC12); err == nil {
		t.Fatal("rc12 fixture no longer reproduces the invalid kind=other card")
	}
	rc12p1, diags := rc11Compile(t, must(t, env), must(t, m), RulesVersionRC12P1)
	found := false
	for _, obs := range rc12Obs(rc12p1, "other") {
		if rc12Summary(obs) == "后续还是要等家里人商量好" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rc12.1 did not publish the kind=other observation: %+v", rc12p1["recording_observations"])
	}
	for _, d := range diags {
		if strings.HasPrefix(d.Path, "$.local_observations") && d.Code != "local_observation_truncated" {
			t.Fatalf("rc12.1 diagnosed a valid local observation: %+v", d)
		}
	}
}

// TestRC12P1PrioritisesTimingUnderBudget makes the budget explicit: on a very
// long recording the rc12.1 budget must keep timing/willingness/exam fragments
// before generic staff chatter.
func TestRC12P1PrioritisesTimingUnderBudget(t *testing.T) {
	in, mid := v210Fixture(t)
	var env, m map[string]any
	if err := json.Unmarshal(in, &env); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mid, &m); err != nil {
		t.Fatal(err)
	}
	sentences := []string{}
	for i := 1; i <= 60; i++ {
		sentences = append(sentences, fmt.Sprintf("第%d句内容是客户在说明情况。", i))
	}
	long := strings.Join(sentences, "")
	for _, raw := range env["input"].(map[string]any)["transcript_sources"].([]any) {
		r := raw.(map[string]any)
		if r["id"] == "s2" {
			r["text"] = long
		}
	}
	rows := []any{}
	for i := 0; i < 60; i++ {
		kind := "staff_statement"
		row := map[string]any{"kind": kind, "summary": sentences[i], "source_refs": []any{"s2"}}
		if i == 59 {
			row["kind"] = "timing"
			row["timing_kind"] = "relative_window"
		}
		rows = append(rows, row)
	}
	m["local_observations"] = rows
	id, err := v200.SnapshotID(must(t, env["input"]))
	if err != nil {
		t.Fatal(err)
	}
	env["input_snapshot_id"] = id
	m["input_snapshot_id"] = id
	rc12p1, diags := rc11Compile(t, must(t, env), must(t, m), RulesVersionRC12P1)
	if got := len(rc12Obs(rc12p1, "timing")); got != 1 {
		t.Fatalf("rc12.1 budget dropped the last timing fragment: %d", got)
	}
	if got := len(rc12Obs(rc12p1, "staff_statement")); got != rc12P1MaxLocalObservations-1 {
		t.Fatalf("rc12.1 budget published %d staff statements", got)
	}
	truncated := false
	for _, d := range diags {
		truncated = truncated || d.Code == "local_observations_truncated"
	}
	if !truncated {
		t.Fatalf("missing local_observations_truncated diagnostic: %+v", diags)
	}
}

// TestRC12P1SharesRC12Gates keeps the repair small: without local observations an
// rc12.1 card equals the rc12 card apart from rules_version.
func TestRC12P1SharesRC12Gates(t *testing.T) {
	in, mid := v210Fixture(t)
	rc12, _ := rc11Compile(t, in, mid, RulesVersionRC12)
	rc12p1, _ := rc11Compile(t, in, mid, RulesVersionRC12P1)
	rc12["rules_version"] = RulesVersionRC12P1
	a, _ := json.Marshal(rc12)
	b, _ := json.Marshal(rc12p1)
	if !bytes.Equal(a, b) {
		t.Fatalf("rc12.1 changed rc12 behavior beyond rules_version:\nrc12=%s\nrc12p1=%s", a, b)
	}
}

// TestRC12P1ContractsReuseIntermediate keeps the rc12 intermediate bytes frozen and
// only repairs the final card enum.
func TestRC12P1ContractsReuseIntermediate(t *testing.T) {
	if !IsRulesVersion(RulesVersionRC12P1) {
		t.Fatal("rc12.1 rules version is not executable")
	}
	i12, m12, f12 := ContractsForVersion(RulesVersionRC12)
	i121, m121, f121 := ContractsForVersion(RulesVersionRC12P1)
	if !bytes.Equal(i12, i121) || !bytes.Equal(m12, m121) {
		t.Fatal("rc12.1 changed the rc12 input/intermediate contract")
	}
	if bytes.Equal(f12, f121) {
		t.Fatal("rc12.1 did not repair the final card schema")
	}
	kinds := func(raw []byte) map[string]bool {
		var card map[string]any
		if err := json.Unmarshal(raw, &card); err != nil {
			t.Fatal(err)
		}
		enum := card["properties"].(map[string]any)["recording_observations"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]any)
		seen := map[string]bool{}
		for _, item := range enum {
			seen[item.(string)] = true
		}
		return seen
	}
	if !kinds(f121)["other"] {
		t.Fatal("rc12.1 card enum still lacks other")
	}
	// rc12 itself must stay frozen: batch 09 was produced with it.
	if kinds(f12)["other"] {
		t.Fatal("rc12 card schema was modified; it must stay frozen for batch 09")
	}
}
