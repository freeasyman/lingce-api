package v200

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func fixture(t *testing.T, prefix string) (object, object) {
	t.Helper()
	read := func(name string) object {
		raw, err := os.ReadFile("testdata/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		v, err := decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		return obj(v)
	}
	return read(prefix + "-input"), read(prefix + "-output")
}
func seal(t *testing.T, e, m object) {
	t.Helper()
	hash, err := canonicalSnapshotID(e["input"])
	if err != nil {
		t.Fatal(err)
	}
	e["input_snapshot_id"] = hash
	m["input_snapshot_id"] = hash
}
func encode(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func run(t *testing.T, e, m object) (Result, object) {
	t.Helper()
	seal(t, e, m)
	r, err := Compile(encode(t, e), encode(t, m), RulesVersion)
	if err != nil {
		t.Fatal(err)
	}
	v, err := decode(r.Card)
	if err != nil {
		t.Fatal(err)
	}
	return r, obj(v)
}
func at(v object, keys ...string) object {
	for _, key := range keys {
		v = obj(v[key])
	}
	return v
}
func row(v object, key string, i int) object { return obj(arr(v[key])[i]) }
func payload(e object) object                { return obj(e["input"]) }
func equal(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
func code(r Result, want string) bool {
	for _, d := range r.Diagnostics {
		if d.Code == want {
			return true
		}
	}
	return false
}
func sales(m object) {
	a := obj(m["proposed_action"])
	a["type"] = "sales_follow_up"
	a["due_basis"] = "within_48h"
	a["timing_source_refs"] = []any{}
	d := obj(m["decision"])
	d["state"] = "有意向但未决定"
	d["condition"] = nil
}

func TestFullAndInsufficientCards(t *testing.T) {
	for _, prefix := range []string{"example", "insufficient"} {
		t.Run(prefix, func(t *testing.T) {
			e, m := fixture(t, prefix)
			r, c := run(t, e, m)
			if prefix == "insufficient" {
				equal(t, r.Status, "needs_review")
				equal(t, c["primary_action"], nil)
				equal(t, c["evidence_refs"], []any{})
				return
			}
			equal(t, r.Status, "analyzed")
			equal(t, at(c, "decision")["decision_state"], "暂缓")
			equal(t, at(c, "decision")["state_basis"], "transcript_judgment")
			equal(t, len(arr(c["evidence_refs"])), 5)
			equal(t, row(c, "evidence_refs", 4)["source_kind"], "business_fact")
			equal(t, row(c, "evidence_refs", 4)["start_ms"], nil)
			equal(t, at(c, "primary_action")["suggested_content"], nil)
			equal(t, at(c, "primary_action")["due_provenance"], "customer_source")
			equal(t, at(c, "decision")["condition_due_quote"], str(row(payload(e), "transcript_sources", 1)["text"])+"\n"+str(row(payload(e), "transcript_sources", 3)["text"]))
			if bytes.Contains(r.Card, []byte("source_refs")) {
				t.Fatal("intermediate source_refs leaked")
			}
		})
	}
}

func TestWholeRequestErrors(t *testing.T) {
	cases := []struct {
		name, stage string
		mutate      func(object, object)
	}{
		{"missing_decision", "intermediate_structure", func(e, m object) { delete(m, "decision") }},
		{"null_ref_array", "intermediate_structure", func(e, m object) { obj(m["decision"])["source_refs"] = nil }},
		{"invented_enum", "intermediate_structure", func(e, m object) { obj(m["decision"])["state"] = "已成交" }},
		{"model_ev", "intermediate_structure", func(e, m object) { obj(m["decision"])["source_refs"] = []any{"ev_1"} }},
		{"model_quote", "intermediate_structure", func(e, m object) { obj(m["decision"])["quote"] = "fake" }},
		{"model_used_for", "intermediate_structure", func(e, m object) { m["used_for"] = []any{"结论"} }},
		{"duplicate_ref", "intermediate_structure", func(e, m object) { obj(m["decision"])["source_refs"] = []any{"s2", "s2"} }},
		{"fractional_id", "input_structure", func(e, m object) { at(payload(e), "recording")["recording_id"] = json.Number("1.5") }},
		{"decimal_integer", "input_binding", func(e, m object) { at(payload(e), "recording")["recording_id"] = json.Number("1001.0") }},
		{"duplicate_source", "input_binding", func(e, m object) {
			p := payload(e)
			p["transcript_sources"] = append(arr(p["transcript_sources"]), row(p, "transcript_sources", 0))
		}},
		{"duplicate_catalog", "input_binding", func(e, m object) {
			p := payload(e)
			p["product_catalog"] = append(arr(p["product_catalog"]), row(p, "product_catalog", 0))
		}},
		{"false_complete", "input_binding", func(e, m object) { at(payload(e), "context")["truncated"] = true }},
		{"unknown_time_with_number", "input_binding", func(e, m object) { row(payload(e), "transcript_sources", 0)["timing_basis"] = "unknown" }},
		{"known_time_without_number", "input_binding", func(e, m object) { row(payload(e), "transcript_sources", 0)["start_ms"] = nil }},
		{"unknown_role_with_label", "input_binding", func(e, m object) { row(payload(e), "transcript_sources", 0)["role_basis"] = "unknown" }},
		{"invalid_timestamp", "input_structure", func(e, m object) { at(payload(e), "context")["analysis_at"] = "tomorrow" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e, m := fixture(t, "example")
			tt.mutate(e, m)
			hash, err := canonicalSnapshotID(payload(e))
			if err == nil {
				e["input_snapshot_id"] = hash
				m["input_snapshot_id"] = hash
			}
			_, err = Compile(encode(t, e), encode(t, m), RulesVersion)
			var ce *Error
			if !errors.As(err, &ce) || ce.Stage != tt.stage {
				t.Fatalf("got %v want stage %s", err, tt.stage)
			}
		})
	}
	t.Run("snapshot_mismatch", func(t *testing.T) {
		e, m := fixture(t, "example")
		obj(m["decision"])["state"] = "无法判断"
		m["input_snapshot_id"] = "sha256:" + strings.Repeat("0", 64)
		_, err := Compile(encode(t, e), encode(t, m), RulesVersion)
		if err == nil {
			t.Fatal("accepted snapshot mismatch")
		}
	})
	for _, raw := range []string{`{`, `{} {}`, `{"x":1,"x":2}`, `{"a":{"x":1,"x":2}}`} {
		t.Run("bad_json_"+raw, func(t *testing.T) {
			e, m := fixture(t, "example")
			for i := 0; i < 2; i++ {
				a, b := encode(t, e), encode(t, m)
				if i == 0 {
					a = []byte(raw)
				} else {
					b = []byte(raw)
				}
				if _, err := Compile(a, b, RulesVersion); err == nil {
					t.Fatal("accepted bad json")
				}
			}
		})
	}
	if _, err := Compile(nil, nil, "future-version"); err == nil {
		t.Fatal("accepted unsupported rules")
	}
}

func TestLocalDegradation(t *testing.T) {
	type assertion func(*testing.T, Result, object)
	noAction := func(t *testing.T, r Result, c object) {
		equal(t, r.Status, "needs_review")
		equal(t, c["primary_action"], nil)
		equal(t, at(c, "decision")["decision_state"], "无法判断")
	}
	cases := []struct {
		name   string
		mutate func(object, object)
		check  assertion
	}{
		{"unknown_core", func(e, m object) { at(m, "decision")["source_refs"] = []any{"s999"} }, noAction},
		{"empty_core", func(e, m object) { at(m, "decision")["source_refs"] = []any{} }, noAction},
		{"wrong_core_kind", func(e, m object) { at(m, "decision")["source_refs"] = []any{"f1"} }, noAction},
		{"missing_condition", func(e, m object) { at(m, "decision")["condition"] = nil }, noAction},
		{"unknown_condition", func(e, m object) { at(m, "decision", "condition")["source_refs"] = []any{"s99"} }, noAction},
		{"bad_quality", func(e, m object) {
			q := at(m, "quality", "dimensions", "开场建立权威")
			q["source_refs"] = []any{"f1"}
			q["impact_on_deal"] = "invented impact"
		}, func(t *testing.T, r Result, c object) {
			q := at(c, "quality", "dimensions", "开场建立权威")
			equal(t, q["assessment"], "insufficient_evidence")
			equal(t, q["score"], nil)
			equal(t, q["impact_on_deal"], nil)
			equal(t, r.Status, "analyzed")
		}},
		{"empty_quality", func(e, m object) { at(m, "quality", "dimensions", "开场建立权威")["source_refs"] = []any{} }, func(t *testing.T, r Result, c object) {
			equal(t, at(c, "quality", "dimensions", "开场建立权威")["score"], nil)
		}},
		{"no_product_evidence", func(e, m object) { row(m, "product_findings", 0)["source_refs"] = []any{"s999"} }, func(t *testing.T, r Result, c object) {
			equal(t, c["product_findings"], []any{})
			equal(t, r.Status, "analyzed")
		}},
		{"no_concern_evidence", func(e, m object) { row(at(m, "decision"), "concerns", 0)["source_refs"] = []any{} }, func(t *testing.T, r Result, c object) { equal(t, at(c, "decision")["concerns"], []any{}) }},
		{"no_trust_need", func(e, m object) {
			row(at(m, "decision"), "trust", 0)["source_refs"] = []any{"s999"}
			at(m, "decision", "need")["source_refs"] = []any{"f1"}
		}, func(t *testing.T, r Result, c object) {
			equal(t, at(c, "decision")["trust"], []any{})
			equal(t, at(c, "decision")["need_summary"], nil)
			equal(t, at(c, "decision")["need_state"], "无法判断")
		}},
		{"no_coach", func(e, m object) {
			row(at(m, "coach"), "successful_practices", 0)["source_refs"] = []any{}
			at(m, "coach", "improvement")["source_refs"] = []any{"f1"}
		}, func(t *testing.T, r Result, c object) {
			equal(t, at(c, "coach")["successful_practices"], []any{})
			equal(t, at(c, "coach")["improvement"], nil)
		}},
		{"no_secondary", func(e, m object) { row(m, "secondary_findings", 0)["source_refs"] = []any{"s99"} }, func(t *testing.T, r Result, c object) { equal(t, c["secondary_findings"], []any{}) }},
		{"no_outcome", func(e, m object) { row(at(m, "decision"), "outcome_signals", 0)["source_refs"] = []any{"f1"} }, func(t *testing.T, r Result, c object) { equal(t, at(c, "decision")["outcome_signals"], []any{}) }},
		{"mixed_refs_keep_valid", func(e, m object) { at(m, "decision")["source_refs"] = []any{"s999", "s4", "s2"} }, func(t *testing.T, r Result, c object) {
			equal(t, r.Status, "analyzed")
			equal(t, at(c, "decision")["evidence_refs"], []any{"ev_2", "ev_4"})
			if !code(r, "unknown_source") {
				t.Fatal("missing path diagnostic")
			}
		}},
		{"multiple_primary", func(e, m object) {
			d := at(m, "decision")
			d["concerns"] = append(arr(d["concerns"]), row(d, "concerns", 0))
		}, func(t *testing.T, r Result, c object) {
			equal(t, r.Status, "needs_review")
			equal(t, c["primary_action"], nil)
			for _, raw := range arr(at(c, "decision")["concerns"]) {
				equal(t, obj(raw)["is_primary"], false)
			}
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e, m := fixture(t, "example")
			tt.mutate(e, m)
			r, c := run(t, e, m)
			tt.check(t, r, c)
		})
	}
	for _, text := range []string{"   ", "……！？", "[转写过长，已截断]", "客户话语[转写过长，已截断]"} {
		t.Run("noise_"+text, func(t *testing.T) {
			e, m := fixture(t, "example")
			at(m, "decision")["source_refs"] = []any{"s2"}
			row(payload(e), "transcript_sources", 1)["text"] = text
			r, c := run(t, e, m)
			noAction(t, r, c)
			if !code(r, "noise_source") {
				t.Fatal("missing noise diagnostic")
			}
		})
	}
}

func TestFactsAndReportedOutcomes(t *testing.T) {
	for _, pair := range [][2]string{{"已预约", "预约"}, {"已交定金", "定金"}, {"已付款", "付款"}} {
		for _, verified := range []bool{true, false} {
			t.Run(pair[0]+strconv.FormatBool(verified), func(t *testing.T) {
				e, m := fixture(t, "example")
				d := at(m, "decision")
				d["state"] = pair[0]
				d["condition"] = nil
				d["source_refs"] = []any{"s2"}
				if verified {
					row(payload(e), "business_facts", 0)["type"] = pair[1]
				} else {
					payload(e)["business_facts"] = []any{}
				}
				_, c := run(t, e, m)
				basis := "reported_only"
				if verified {
					basis = "verified_fact"
				}
				equal(t, at(c, "decision")["state_basis"], basis)
				if !verified {
					equal(t, at(c, "conclusion")["business_impact"], "无法判断")
				}
			})
		}
	}
	t.Run("wrong_fact_type", func(t *testing.T) {
		e, m := fixture(t, "example")
		at(m, "decision")["state"] = "已付款"
		at(m, "decision")["condition"] = nil
		at(m, "decision")["source_refs"] = []any{"f1"}
		r, c := run(t, e, m)
		equal(t, r.Status, "needs_review")
		equal(t, at(c, "decision")["state_basis"], "unknown")
	})
	t.Run("payment_does_not_erase_intent", func(t *testing.T) {
		e, m := fixture(t, "example")
		row(payload(e), "business_facts", 0)["type"] = "付款"
		_, c := run(t, e, m)
		equal(t, at(c, "decision")["decision_state"], "暂缓")
		equal(t, row(c, "business_facts", 0)["type"], "付款")
	})
	t.Run("refund_is_not_reason_or_net_deal", func(t *testing.T) {
		e, m := fixture(t, "example")
		f := object{"id": "f2", "type": "退款", "text": "已核实退款", "source_record_id": json.Number("702"), "occurred_at": nil}
		payload(e)["business_facts"] = append(arr(payload(e)["business_facts"]), f)
		at(m, "coach")["refund_review"] = object{"known_reason": "顾问服务差", "source_refs": []any{"f2"}, "unknowns": []any{}}
		r, c := run(t, e, m)
		equal(t, r.Status, "needs_review")
		equal(t, c["primary_action"], nil)
		equal(t, at(c, "conclusion")["business_impact"], "无法判断")
		equal(t, at(c, "coach", "refund_review")["known_reason"], nil)
		equal(t, len(arr(c["business_facts"])), 2)
	})
}

func TestContactAndClinicalPriority(t *testing.T) {
	cases := []struct {
		name, policy string
		mutate       func(object, object)
		action       bool
	}{
		{"normal", "allow", func(e, m object) {}, true},
		{"stop", "stop", func(e, m object) {
			at(m, "contact_request")["value"] = "stop"
			at(m, "contact_request")["source_refs"] = []any{"s4"}
		}, false},
		{"system_stop_over_clinical", "stop", func(e, m object) {
			at(payload(e), "context")["contact_forbidden"] = true
			at(payload(e), "context")["clinical_hold"] = true
		}, false},
		{"invalid_stop", "review", func(e, m object) {
			at(m, "contact_request")["value"] = "stop"
			at(m, "contact_request")["source_refs"] = []any{"s999"}
		}, false},
		{"clinical_sales_block", "pause_clinical", func(e, m object) { at(payload(e), "context")["clinical_hold"] = true }, false},
		{"clinical_internal_coordination", "pause_clinical", func(e, m object) {
			at(payload(e), "context")["clinical_hold"] = true
			at(m, "proposed_action")["type"] = "internal_coordination"
		}, true},
		{"clinical_internal_review_block", "pause_clinical", func(e, m object) {
			at(payload(e), "context")["clinical_hold"] = true
			at(m, "proposed_action")["type"] = "internal_review"
		}, false},
		{"unresolved", "pause_clinical", func(e, m object) {
			r := at(m, "clinical_risk")
			r["assessment"] = "unresolved"
			r["source_refs"] = []any{"s3"}
			r["summary"] = "待医生核实"
		}, false},
		{"invalid_unresolved_keeps_hold", "pause_clinical", func(e, m object) {
			at(m, "clinical_risk")["assessment"] = "unresolved"
			at(m, "clinical_risk")["source_refs"] = []any{}
			at(m, "proposed_action")["type"] = "internal_coordination"
		}, false},
		{"unauthorized", "review", func(e, m object) { at(payload(e), "context")["contact_authorized"] = false }, false},
		{"authorization_unknown", "review", func(e, m object) { at(payload(e), "context")["contact_authorized"] = nil }, false},
		{"identity_unknown", "review", func(e, m object) { at(payload(e), "context")["identity_confirmed"] = false }, false},
		{"low_confidence", "review", func(e, m object) { m["confidence"] = "low" }, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e, m := fixture(t, "example")
			sales(m)
			payload(e)["business_facts"] = []any{}
			tt.mutate(e, m)
			_, c := run(t, e, m)
			equal(t, c["contact_policy"], tt.policy)
			equal(t, c["primary_action"] != nil, tt.action)
			if !tt.action && strings.Contains(str(at(c, "brief")["next_step"]), "安排跟进") {
				t.Fatal("brief reintroduced blocked sales action")
			}
		})
	}
}

func TestActionTiming(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(object, object)
		kept       bool
		provenance string
	}{
		{"system_window", func(e, m object) {}, true, "system_suggestion"},
		{"customer_window", func(e, m object) { at(m, "proposed_action")["timing_source_refs"] = []any{"s4"} }, true, "customer_source"},
		{"unknown_timing", func(e, m object) { at(m, "proposed_action")["timing_source_refs"] = []any{"s999"} }, false, ""},
		{"consultant_window", func(e, m object) { at(m, "proposed_action")["timing_source_refs"] = []any{"s3"} }, false, ""},
		{"mixed_consultant_window", func(e, m object) { at(m, "proposed_action")["timing_source_refs"] = []any{"s3", "s4"} }, false, ""},
		{"partially_invalid_window", func(e, m object) { at(m, "proposed_action")["timing_source_refs"] = []any{"s999", "s4"} }, false, ""},
		{"fact_window", func(e, m object) { at(m, "proposed_action")["timing_source_refs"] = []any{"f1"} }, false, ""},
		{"event_without_evidence", func(e, m object) { at(m, "proposed_action")["due_basis"] = "after_family_discussion" }, false, ""},
		{"deferred_without_window", func(e, m object) {
			at(m, "decision")["state"] = "暂缓"
			at(m, "decision")["condition"] = object{"summary": "等待家属", "source_refs": []any{"s4"}}
		}, false, ""},
		{"no_action_evidence", func(e, m object) { at(m, "proposed_action")["source_refs"] = []any{} }, false, ""},
		{"explicit_rejection", func(e, m object) { at(m, "decision")["state"] = "明确不考虑" }, false, ""},
		{"unknown_role", func(e, m object) {
			for _, i := range []int{1, 3} {
				s := row(payload(e), "transcript_sources", i)
				s["speaker"] = "未知"
				s["role_basis"] = "unknown"
			}
		}, false, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e, m := fixture(t, "example")
			sales(m)
			payload(e)["business_facts"] = []any{}
			tt.mutate(e, m)
			_, c := run(t, e, m)
			equal(t, c["primary_action"] != nil, tt.kept)
			if tt.kept {
				a := at(c, "primary_action")
				equal(t, a["due_provenance"], tt.provenance)
				equal(t, a["owner_employee_id"], nil)
				if tt.provenance == "system_suggestion" {
					equal(t, a["due_quote"], nil)
					equal(t, a["timing_evidence_refs"], []any{})
				}
			}
		})
	}
}

func TestProductsAndPartialCoverage(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(object, object)
		kept   bool
	}{
		{"catalog_enriched", func(e, m object) {}, true},
		{"unknown_id", func(e, m object) { row(m, "product_findings", 0)["catalog_product_id"] = json.Number("999") }, false},
		{"no_id_or_name", func(e, m object) { row(m, "product_findings", 0)["catalog_product_id"] = nil }, false},
		{"name_only", func(e, m object) {
			r := row(m, "product_findings", 0)
			r["catalog_product_id"] = nil
			r["mentioned_name"] = "录音提及方案"
		}, true},
		{"missed_disabled", func(e, m object) {
			row(m, "product_findings", 0)["finding_type"] = "可能漏推"
			at(payload(e), "context")["allow_missed_opportunity"] = false
		}, false},
		{"partial_missed", func(e, m object) {
			row(m, "product_findings", 0)["finding_type"] = "可能漏推"
			at(payload(e), "context")["coverage"] = "partial"
		}, false},
		{"incomplete_catalog_missed", func(e, m object) {
			row(m, "product_findings", 0)["finding_type"] = "可能漏推"
			at(payload(e), "context")["catalog_complete"] = false
		}, false},
		{"joint_disabled", func(e, m object) { row(m, "product_findings", 0)["finding_type"] = "联合机会" }, false},
		{"extended_enabled", func(e, m object) {
			row(m, "product_findings", 0)["finding_type"] = "升级机会"
			at(payload(e), "context")["allow_extended_opportunities"] = true
		}, true},
		{"clinical", func(e, m object) { at(payload(e), "context")["clinical_hold"] = true }, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e, m := fixture(t, "example")
			tt.mutate(e, m)
			_, c := run(t, e, m)
			equal(t, len(arr(c["product_findings"])) > 0, tt.kept)
			if tt.kept {
				p := row(c, "product_findings", 0)
				if p["product_id"] != nil {
					equal(t, p["product_name"], row(payload(e), "product_catalog", 0)["name"])
					equal(t, p["product_version"], "1")
				} else {
					equal(t, p["requires_clinical_confirmation"], true)
				}
			}
		})
	}
	t.Run("partial_withholds_scores", func(t *testing.T) {
		e, m := fixture(t, "example")
		at(payload(e), "context")["coverage"] = "partial"
		at(payload(e), "context")["truncated"] = true
		r, c := run(t, e, m)
		equal(t, at(c, "quality", "dimensions", "开场建立权威")["score"], nil)
		equal(t, at(c, "clinical_conflict")["assessment"], "unknown")
		if !code(r, "incomplete_coverage_score_withheld") {
			t.Fatal("missing coverage diagnostic")
		}
	})
}

func TestEvidenceExactnessAndStability(t *testing.T) {
	e, m := fixture(t, "example")
	s := row(payload(e), "transcript_sources", 0)
	s["start_ms"] = nil
	s["timing_basis"] = "unknown"
	s["speaker"] = "未知"
	s["role_basis"] = "unknown"
	_, c := run(t, e, m)
	sources := map[string]object{}
	for _, raw := range arr(payload(e)["transcript_sources"]) {
		r := obj(raw)
		sources[str(r["id"])] = r
	}
	for _, raw := range arr(c["evidence_refs"]) {
		ev := obj(raw)
		if ev["source_kind"] != "transcript" {
			continue
		}
		src := sources[str(ev["source_ref"])]
		for _, k := range []string{"speaker", "start_ms", "timing_basis", "role_basis"} {
			equal(t, ev[k], src[k])
		}
		equal(t, ev["quote"], src["text"])
	}
	baseline := encode(t, c["evidence_refs"])
	at(m, "decision")["source_refs"] = []any{"s4", "s2"}
	row(m, "product_findings", 0)["source_refs"] = []any{"s4", "s3", "s2"}
	_, again := run(t, e, m)
	equal(t, string(encode(t, again["evidence_refs"])), string(baseline))
	// Removing an unrelated early source use must not renumber later sources.
	at(m, "quality", "dimensions", "开场建立权威")["source_refs"] = []any{}
	_, again = run(t, e, m)
	equal(t, row(again, "evidence_refs", 0)["id"], "ev_2")
}

func TestReplayConcurrentAndNoInputMutation(t *testing.T) {
	e, m := fixture(t, "example")
	a, b := encode(t, e), encode(t, m)
	a0, b0 := bytes.Clone(a), bytes.Clone(b)
	first, err := Compile(a, b, RulesVersion)
	if err != nil {
		t.Fatal(err)
	}
	want := encode(t, first)
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := Compile(a, b, RulesVersion)
			if err != nil {
				errs <- err
				return
			}
			raw, err := json.Marshal(r)
			if err != nil {
				errs <- err
			} else if !bytes.Equal(raw, want) {
				errs <- fmt.Errorf("non-deterministic compile")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	equal(t, string(a), string(a0))
	equal(t, string(b), string(b0))
}

func TestCanonicalUnicodeAndLargeInteger(t *testing.T) {
	p, err := decode([]byte(`{"z":9007199254740993,"a":"中文<&>\u2028\u2029","b":-0}`))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := canonicalSnapshotID(p)
	if err != nil {
		t.Fatal(err)
	}
	// Python json.dumps(ensure_ascii=False,sort_keys=True,separators=(',',':'))
	// sha256 of the same integer-only payload, preserving >2^53 integers.
	equal(t, hash, "sha256:62b7067cd14138e5b7c643ef17149a5b673fe3951418b9ea30b5546be40c1bfa")
}

func TestCanonicalLiteralEscapePreserved(t *testing.T) {
	p, err := decode([]byte(`{"a":"literal \\u2028 and \\u2029"}`))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := canonicalSnapshotID(p)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, hash, "sha256:b70d97dc30b2f232a0eb6e2d31102f72206d675e97b1ef9d78ca0e735ce7bf84")
}

func TestVerifiedPaymentDoesNotGenerateNewSales(t *testing.T) {
	for _, kind := range []string{"定金", "付款"} {
		t.Run(kind, func(t *testing.T) {
			e, m := fixture(t, "example")
			sales(m)
			row(payload(e), "business_facts", 0)["type"] = kind
			r, c := run(t, e, m)
			equal(t, c["primary_action"], nil)
			if !code(r, "verified_payment_requires_service_review") {
				t.Fatal("payment bypassed")
			}
		})
	}
}

func TestVerifiedPaymentSurvivesCurrentRejection(t *testing.T) {
	e, m := fixture(t, "example")
	sales(m)
	at(m, "decision")["state"] = "明确不考虑"
	_, c := run(t, e, m)
	equal(t, at(c, "decision")["decision_state"], "明确不考虑")
	equal(t, at(c, "conclusion")["business_impact"], "已成交可复盘")
	equal(t, c["primary_action"], nil)
}

func TestNoiseCannotSupplyCustomerRole(t *testing.T) {
	e, m := fixture(t, "example")
	sales(m)
	payload(e)["business_facts"] = []any{}
	row(payload(e), "transcript_sources", 1)["text"] = "……"
	at(m, "decision")["source_refs"] = []any{"s2", "s3"}
	r, c := run(t, e, m)
	equal(t, c["primary_action"], nil)
	if !code(r, "sales_customer_role_unconfirmed") {
		t.Fatal("noise unlocked sales")
	}
}

func TestNoPanicOnMalformedInput(t *testing.T) {
	e, m := fixture(t, "example")
	a, b := encode(t, e), encode(t, m)
	for _, raw := range [][]byte{[]byte(`null`), []byte(`[]`), []byte(`false`), []byte(`{"x":"`), []byte{'"', 0xff, '"'}, []byte(strings.Repeat("[", 130) + strings.Repeat("]", 130))} {
		if _, err := Compile(raw, b, RulesVersion); err == nil {
			t.Fatal("accepted malformed input")
		}
		if _, err := Compile(a, raw, RulesVersion); err == nil {
			t.Fatal("accepted malformed output")
		}
	}
}

func TestFinalContractRejectsBrokenReferences(t *testing.T) {
	e, m := fixture(t, "example")
	_, base := run(t, e, m)
	for _, tt := range []struct {
		name   string
		change func(object)
	}{
		{"dangling", func(c object) { at(c, "decision")["evidence_refs"] = []any{"ev_999"} }},
		{"wrong_usage", func(c object) { row(c, "evidence_refs", 0)["used_for"] = []any{"动作"} }},
		{"fact_with_time", func(c object) { row(c, "evidence_refs", 4)["start_ms"] = json.Number("1") }},
		{"transcript_promoted", func(c object) { row(c, "evidence_refs", 0)["speaker"] = "系统事实" }},
		{"blocked_action", func(c object) { c["status"] = "needs_review" }},
		{"false_verified_basis", func(c object) { at(c, "decision")["state_basis"] = "verified_fact" }},
		{"false_timing_provenance", func(c object) { at(c, "primary_action")["due_provenance"] = "system_suggestion" }},
		{"fact_source_kind_mismatch", func(c object) { row(c, "evidence_refs", 0)["source_kind"] = "business_fact" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v, err := decode(encode(t, base))
			if err != nil {
				t.Fatal(err)
			}
			c := obj(v)
			tt.change(c)
			if err := ValidateCard(encode(t, c)); err == nil {
				t.Fatal("accepted corrupted final card")
			}
		})
	}
}

func TestNoSideEffectDependencies(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"bytes": true, "crypto/sha256": true, "encoding/hex": true, "encoding/json": true, "fmt": true, "io": true, "regexp": true, "sort": true, "strings": true, "sync": true, "unicode": true, "unicode/utf8": true, "embed": true, "github.com/santhosh-tekuri/jsonschema/v6": true}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			pkg, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !allowed[pkg] {
				t.Fatalf("side effect dependency %s in %s", pkg, path)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if fun, ok := call.Fun.(*ast.Ident); ok && fun.Name == "panic" {
					t.Error("production panic")
				}
			}
			return true
		})
	}
}

func TestIgnoredFieldsStillDiagnosedAndCannotPublishSales(t *testing.T) {
	e, m := fixture(t, "example")
	at(m, "conclusion")["summary"] = nil
	at(m, "conclusion")["source_refs"] = []any{"s999"}
	at(m, "conclusion")["verification"] = "立即销售，并已确认全款"
	at(m, "decision")["state"] = "有意向但未决定"
	at(m, "decision", "condition")["source_refs"] = []any{"s998"}
	m["confidence"] = "low"
	r, c := run(t, e, m)
	unknown := 0
	for _, d := range r.Diagnostics {
		if d.Code == "unknown_source" {
			unknown++
		}
	}
	equal(t, unknown, 2)
	if bytes.Contains(r.Card, []byte("立即销售")) {
		t.Fatal("uncompiled narrative bypassed rule")
	}
	equal(t, c["primary_action"], nil)
}

func TestSnapshotPublicAPI(t *testing.T) {
	e, _ := fixture(t, "example")
	id, err := SnapshotID(encode(t, payload(e)))
	if err != nil {
		t.Fatal(err)
	}
	equal(t, id, e["input_snapshot_id"])
	for _, raw := range []string{`null`, `[]`, `{"x":1.2}`, `{"x":1e2}`, `{"x":1,"x":2}`, `{`} {
		if _, err := SnapshotID([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func FuzzCompileRejectsMalformedWithoutPanicking(f *testing.F) {
	a, err := os.ReadFile("testdata/example-input.json")
	if err != nil {
		f.Fatal(err)
	}
	b, err := os.ReadFile("testdata/example-output.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(a, b)
	f.Add([]byte(`null`), []byte(`[]`))
	f.Add([]byte(`{`), b)
	f.Fuzz(func(t *testing.T, snapshot, model []byte) {
		r, err := Compile(snapshot, model, RulesVersion)
		if err == nil {
			if err := ValidateCard(r.Card); err != nil {
				t.Fatal(err)
			}
		}
	})
}
