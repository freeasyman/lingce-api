package v210

import (
	"strings"
	"testing"
)

func spanSupport(kind, ref string, start, end int) object {
	return object{"statement_type": kind, "confidence": "high", "spans": []any{object{"source_ref": ref, "start": start, "end": end}}}
}
func TestV210CustomerChoiceCannotBecomeFear(t *testing.T) {
	in, mid := boundFixture(t)
	e, m := decoded(t, in), decoded(t, mid)
	input := obj(e["input"])
	src := obj(arr(input["transcript_sources"])[1])
	src["text"] = "手术我还算了吧，我先保守治疗两天试试。"
	d := obj(m["decision"])
	r := obj(arr(d["concerns"])[0])
	r["summary"] = "恐惧手术并担心创伤"
	r["support"] = spanSupport("customer_choice", "s2", 0, 8)
	d["trust"] = []any{}
	d["need"] = object{"state": "无法判断", "summary": nil, "source_refs": []any{}, "support": nil}
	in, mid = sealed(t, e, m)
	_, c := compileCard(t, in, mid)
	if len(arr(obj(c["decision"])["concerns"])) != 0 {
		t.Fatal("choice accepted as stated fear")
	}
	for _, raw := range arr(c["recording_observations"]) {
		r := obj(raw)
		if r["kind"] == "concern" {
			t.Fatal("recording path bypassed concern qualification")
		}
	}
}
func TestV210ExplicitFeelingUsesOriginalSpanNotModelSummary(t *testing.T) {
	in, mid := boundFixture(t)
	e, m := decoded(t, in), decoded(t, mid)
	src := obj(arr(obj(e["input"])["transcript_sources"])[1])
	src["text"] = "我害怕手术。"
	d := obj(m["decision"])
	r := obj(arr(d["concerns"])[0])
	r["summary"] = "已经确诊且必须做手术"
	r["support"] = spanSupport("customer_feeling", "s2", 0, 6)
	in, mid = sealed(t, e, m)
	_, c := compileCard(t, in, mid)
	rows := arr(obj(c["decision"])["concerns"])
	if len(rows) != 1 || obj(rows[0])["summary"] != "我害怕手术。" {
		t.Fatal("explicit feeling lost or paraphrase published", rows)
	}
}
func TestV210StatementSupportCannotInventOrRelabelSources(t *testing.T) {
	for _, kind := range []string{"customer_question", "staff_claim", "inference", "unknown", "unrelated_ref", "staff_as_customer"} {
		t.Run(kind, func(t *testing.T) {
			in, mid := boundFixture(t)
			e, m := decoded(t, in), decoded(t, mid)
			d := obj(m["decision"])
			r := obj(arr(d["concerns"])[0])
			r["support"] = spanSupport(kind, "s2", 0, 2)
			switch kind {
			case "unrelated_ref":
				r["support"] = spanSupport("customer_feeling", "s4", 0, 2)
			case "staff_as_customer":
				r["source_refs"] = []any{"s3"}
				r["support"] = spanSupport("customer_feeling", "s3", 0, 2)
			}
			in, mid = sealed(t, e, m)
			_, c := compileCard(t, in, mid)
			if len(arr(obj(c["decision"])["concerns"])) != 0 {
				t.Fatal("invalid support accepted")
			}
		})
	}
}

// P1-1: a rune offset that exceeds the source length only weakens the evidence
// locator. The publication rule still retains the complete bounded customer
// utterance, so one bad offset must not erase a multi-span cross-sentence fact.
func TestV210OutOfRangeSpanKeepsWholeUtterance(t *testing.T) {
	in, mid := boundFixture(t)
	e, m := decoded(t, in), decoded(t, mid)
	sources := arr(obj(e["input"])["transcript_sources"])
	budget := "15万？这点钱我预算了，有点贵。"
	original := "我本来想着10万左右吧，你这一个15万，还差挺多的。"
	obj(sources[1])["text"] = budget
	obj(sources[3])["text"] = original
	d := obj(m["decision"])
	r := obj(arr(d["concerns"])[0])
	r["source_refs"] = []any{"s2", "s4"}
	r["support"] = object{"statement_type": "customer_constraint", "confidence": "high", "spans": []any{
		object{"source_ref": "s2", "start": 0, "end": len([]rune(budget)) + 1},
		object{"source_ref": "s4", "start": 0, "end": len([]rune(original))},
	}}
	in, mid = sealed(t, e, m)
	res, c := compileCard(t, in, mid)
	rows := arr(obj(c["decision"])["concerns"])
	if len(rows) != 1 {
		t.Fatalf("out-of-range span dropped the whole multi-span fact: %#v", rows)
	}
	got := str(obj(rows[0])["summary"])
	if got != budget+"\n"+original || !strings.Contains(got, "10万") {
		t.Fatalf("publication did not retain every complete utterance: %q", got)
	}
	degraded := false
	for _, raw := range res.Diagnostics {
		if raw.Code == "statement_span_out_of_range" && raw.Path == "$.decision.concerns[0]" && raw.Severity == "degraded" {
			degraded = true
		}
	}
	if !degraded {
		t.Fatal("out-of-range offset was not recorded as degraded diagnostic")
	}
	for _, raw := range arr(c["evidence_refs"]) {
		ev := obj(raw)
		if ev["source_ref"] == "s4" && !strings.Contains(str(ev["quote"]), "10万") {
			t.Fatal("budget utterance missing from evidence")
		}
	}
}

func TestV210ExtractionPreservesNegationContext(t *testing.T) {
	in, mid := boundFixture(t)
	e, m := decoded(t, in), decoded(t, mid)
	obj(arr(obj(e["input"])["transcript_sources"])[1])["text"] = "我不害怕手术，只是现在没时间。"
	r := obj(arr(obj(m["decision"])["concerns"])[0])
	r["summary"] = "害怕手术"
	r["support"] = spanSupport("customer_feeling", "s2", 2, 6)
	in, mid = sealed(t, e, m)
	_, c := compileCard(t, in, mid)
	rows := arr(obj(c["decision"])["concerns"])
	if len(rows) != 1 || obj(rows[0])["summary"] != "我不害怕手术，只是现在没时间。" {
		t.Fatal("span removed negation or context", rows)
	}
}
func TestV210NeedAndTrustRequireTheirOwnStatementTypes(t *testing.T) {
	for _, kind := range []string{"customer_question", "staff_claim", "customer_choice", "customer_feeling", "customer_trust", "customer_need"} {
		t.Run(kind, func(t *testing.T) {
			in, mid := boundFixture(t)
			e, m := decoded(t, in), decoded(t, mid)
			obj(arr(obj(e["input"])["transcript_sources"])[1])["text"] = "我想先了解价格。我信任这位医生。"
			d := obj(m["decision"])
			n := obj(d["need"])
			n["support"] = spanSupport(kind, "s2", 0, 8)
			tr := obj(arr(d["trust"])[0])
			tr["source_refs"] = []any{"s2"}
			tr["support"] = spanSupport(kind, "s2", 8, 16)
			in, mid = sealed(t, e, m)
			_, c := compileCard(t, in, mid)
			got := obj(c["decision"])
			if kind == "customer_need" && got["need_summary"] != "我想先了解价格。我信任这位医生。" {
				t.Fatal("explicit need lost", got)
			}
			if kind == "customer_trust" && len(arr(got["trust"])) != 1 {
				t.Fatal("explicit trust lost", got)
			}
			if kind != "customer_need" && got["need_summary"] != nil {
				t.Fatal("unrelated type became need")
			}
			if kind != "customer_trust" && len(arr(got["trust"])) != 0 {
				t.Fatal("unrelated type became trust")
			}
		})
	}
}

func TestV210UnknownIntentDoesNotEraseExplicitConstraint(t *testing.T) {
	in, mid := boundFixture(t)
	e, m := decoded(t, in), decoded(t, mid)
	obj(arr(obj(e["input"])["transcript_sources"])[1])["text"] = "我不害怕手术，只是现在没时间。"
	d := obj(m["decision"])
	d["state"] = "无法判断"
	r := obj(arr(d["concerns"])[0])
	r["support"] = spanSupport("customer_constraint", "s2", 0, 10)
	in, mid = sealed(t, e, m)
	_, c := compileCard(t, in, mid)
	if len(arr(obj(c["decision"])["concerns"])) != 1 {
		t.Fatal("unknown intent erased explicit concern")
	}
}

func TestV210MixedIntentRefsDoNotEraseBoundCustomerConcern(t *testing.T) {
	in, mid := boundFixture(t)
	e, m := decoded(t, in), decoded(t, mid)
	obj(arr(obj(e["input"])["transcript_sources"])[1])["text"] = "我害怕手术。"
	d := obj(m["decision"])
	d["state"] = "无法判断"
	d["source_refs"] = []any{"s2", "s3"}
	r := obj(arr(d["concerns"])[0])
	r["support"] = spanSupport("customer_feeling", "s2", 0, 6)
	in, mid = sealed(t, e, m)
	_, c := compileCard(t, in, mid)
	if len(arr(obj(c["decision"])["concerns"])) != 1 {
		t.Fatal("mixed intent erased separately bound explicit concern")
	}
}

func TestV210ConflictingTypeForSameSpanCannotPromoteNeed(t *testing.T) {
	in, mid := boundFixture(t)
	m := decoded(t, mid)
	d := obj(m["decision"])
	n := obj(d["need"])
	n["support"] = spanSupport("customer_choice", "s2", 0, 6)
	other := decoded(t, must(t, d))
	other["target"] = "方案乙"
	obj(other["need"])["support"] = spanSupport("customer_need", "s2", 0, 6)
	m["additional_decisions"] = []any{other}
	_, c := compileCard(t, in, must(t, m))
	for _, raw := range arr(c["scheme_decisions"]) {
		if obj(raw)["need_summary"] != nil {
			t.Fatal("contradictory classification promoted to need")
		}
	}
	for _, raw := range arr(c["recording_observations"]) {
		if obj(raw)["kind"] == "need" {
			t.Fatal("contradictory classification escaped via local path")
		}
	}
}
