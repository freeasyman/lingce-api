package v200

import "testing"

// Phase 4 exercises business boundaries against synthetic conversation text.
// The deliberately false model judgments below also document that reference
// integrity cannot prove semantic entailment. They are not semantic gold passes.
func TestPhase4EvidenceAndPolicyRegressions(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(object, object)
		check  func(*testing.T, Result, object)
	}{
		{"authorized_sales_positive", func(e, m object) {}, func(t *testing.T, r Result, c object) {
			equal(t, r.Status, "analyzed")
			equal(t, c["contact_policy"], "allow")
			equal(t, c["primary_action"] != nil, true)
		}},
		{"nonexistent_core", func(e, m object) { at(m, "decision")["source_refs"] = []any{"s999"} }, func(t *testing.T, r Result, c object) {
			equal(t, r.Status, "needs_review")
			equal(t, c["primary_action"], nil)
			if !code(r, "unknown_source") {
				t.Fatal("missing diagnostic")
			}
		}},
		{"empty_source", func(e, m object) { row(payload(e), "transcript_sources", 1)["text"] = "  " }, func(t *testing.T, r Result, c object) {
			equal(t, r.Status, "needs_review")
			equal(t, c["primary_action"], nil)
		}},
		{"noise_source", func(e, m object) { row(payload(e), "transcript_sources", 1)["text"] = "……！？" }, func(t *testing.T, r Result, c object) {
			equal(t, r.Status, "needs_review")
			equal(t, c["primary_action"], nil)
		}},
		{"control_source", func(e, m object) {
			row(payload(e), "transcript_sources", 1)["text"] = "[转写过长，已截断]"
			at(payload(e), "context")["coverage"] = "partial"
			at(payload(e), "context")["truncated"] = true
		}, func(t *testing.T, r Result, c object) {
			equal(t, r.Status, "needs_review")
			equal(t, c["primary_action"], nil)
		}},
		{"unknown_customer_role", func(e, m object) {
			s := row(payload(e), "transcript_sources", 1)
			s["speaker"] = "未知"
			s["role_basis"] = "unknown"
		}, func(t *testing.T, r Result, c object) {
			equal(t, c["primary_action"], nil)
			if !code(r, "sales_customer_role_unconfirmed") {
				t.Fatal("unknown role accepted for sales")
			}
		}},
		{"consultant_cannot_commit_customer", func(e, m object) { row(payload(e), "transcript_sources", 1)["speaker"] = "咨询师" }, func(t *testing.T, r Result, c object) {
			equal(t, c["primary_action"], nil)
			if !code(r, "sales_customer_role_unconfirmed") {
				t.Fatal("consultant accepted as customer")
			}
		}},
		{"doctor_cannot_commit_customer", func(e, m object) { row(payload(e), "transcript_sources", 1)["speaker"] = "医生" }, func(t *testing.T, r Result, c object) {
			equal(t, c["primary_action"], nil)
		}},
		{"spoken_payment_is_not_fact", func(e, m object) {
			row(payload(e), "transcript_sources", 1)["text"] = "我已经付完款了。"
			at(m, "decision")["state"] = "已付款"
		}, func(t *testing.T, r Result, c object) {
			equal(t, at(c, "decision")["state_basis"], "reported_only")
			equal(t, c["primary_action"], nil)
			equal(t, at(c, "conclusion")["business_impact"], "无法判断")
		}},
		{"quality_without_evidence", func(e, m object) { at(m, "quality", "dimensions", "开场建立权威")["source_refs"] = []any{} }, func(t *testing.T, r Result, c object) {
			equal(t, at(c, "quality", "dimensions", "开场建立权威")["score"], nil)
			equal(t, r.Status, "analyzed")
		}},
		{"short_partial_recording", func(e, m object) { at(payload(e), "context")["coverage"] = "partial" }, func(t *testing.T, r Result, c object) {
			for _, raw := range at(c, "quality", "dimensions") {
				equal(t, obj(raw)["score"], nil)
			}
		}},
		{"multiple_local_missing", func(e, m object) {
			row(m, "product_findings", 0)["source_refs"] = []any{"s999"}
			row(at(m, "decision"), "concerns", 0)["source_refs"] = []any{}
			row(at(m, "coach"), "successful_practices", 0)["source_refs"] = []any{"s999"}
		}, func(t *testing.T, r Result, c object) {
			equal(t, r.Status, "analyzed")
			equal(t, c["product_findings"], []any{})
			equal(t, at(c, "decision")["concerns"], []any{})
			equal(t, at(c, "coach")["successful_practices"], []any{})
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e, m := fixture(t, "example")
			sales(m)
			payload(e)["business_facts"] = []any{}
			at(m, "decision")["source_refs"] = []any{"s2"}
			row(payload(e), "transcript_sources", 1)["text"] = "我想进一步了解这个方案，请过两天联系我。"
			at(m, "proposed_action")["source_refs"] = []any{"s2"}
			tt.mutate(e, m)
			r, c := run(t, e, m)
			tt.check(t, r, c)
		})
	}
}
