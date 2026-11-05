package v200

import "fmt"

func (c *compiler) compileObservations(out, d object) {
	trust := []any{}
	for i, raw := range arr(d["trust"]) {
		r := obj(raw)
		p := fmt.Sprintf("$.decision.trust[%d]", i)
		refs := c.refs(r, "source_refs", p, "transcript")
		if len(refs) == 0 {
			c.missing(p)
			continue
		}
		trust = append(trust, object{"trust_target": r["target"], "target_label": r["target_label"], "state": r["state"], "summary": r["summary"], "evidence_refs": list(refs)})
	}
	out["trust"] = trust
	n := obj(d["need"])
	refs := c.refs(n, "source_refs", "$.decision.need", "transcript")
	if len(refs) > 0 {
		out["need_state"] = n["state"]
		out["need_summary"] = n["summary"]
		out["need_evidence_refs"] = list(refs)
	} else if n["state"] != "无法判断" || n["summary"] != nil {
		c.missing("$.decision.need")
	}
	concerns := []any{}
	primaryCount := 0
	for i, raw := range arr(d["concerns"]) {
		r := obj(raw)
		p := fmt.Sprintf("$.decision.concerns[%d]", i)
		refs := c.refs(r, "source_refs", p, "transcript")
		if len(refs) == 0 {
			c.missing(p)
			continue
		}
		if r["is_primary"] == true {
			primaryCount++
		}
		concerns = append(concerns, object{"type": r["type"], "type_label": r["type_label"], "summary": r["summary"], "is_primary": r["is_primary"], "evidence_refs": list(refs)})
	}
	if primaryCount > 1 {
		c.review = true
		c.note("$.decision.concerns", "conflicting_primary_concerns")
		for _, r := range concerns {
			obj(r)["is_primary"] = false
		}
	}
	out["concerns"] = concerns
	signals := []any{}
	for i, raw := range arr(d["outcome_signals"]) {
		r := obj(raw)
		p := fmt.Sprintf("$.decision.outcome_signals[%d]", i)
		refs := c.refs(r, "source_refs", p, "transcript")
		if len(refs) == 0 {
			c.missing(p)
			continue
		}
		signals = append(signals, object{"type": r["type"], "summary": r["summary"], "evidence_refs": list(refs)})
	}
	out["outcome_signals"] = signals
}

func (c *compiler) observations(rows []any, path string) []any {
	out := []any{}
	for i, raw := range rows {
		r := obj(raw)
		p := fmt.Sprintf("%s[%d]", path, i)
		refs := c.refs(r, "source_refs", p, "")
		if len(refs) == 0 {
			c.missing(p)
			continue
		}
		out = append(out, object{"summary": r["summary"], "evidence_refs": list(refs)})
	}
	return out
}

var qualityNames = []string{"开场建立权威", "需求探索", "问题放大", "专业呈现", "方案定制", "异议化解", "促成与收尾"}

func (c *compiler) compileQuality() object {
	q := obj(c.model["quality"])
	dimensions := obj(q["dimensions"])
	out := object{}
	for _, name := range qualityNames {
		r := dimensions[name].(object)
		path := "$.quality.dimensions." + name
		refs := c.refs(r, "source_refs", path, "transcript")
		item := object{"assessment": r["assessment"], "score": r["score"], "comment": r["comment"], "impact_on_deal": r["impact_on_deal"], "highlight_narrative": r["highlight_narrative"], "evidence_refs": list(refs)}
		if len(refs) == 0 && (r["assessment"] != "insufficient_evidence" || r["impact_on_deal"] != nil) {
			c.missing(path)
			item["assessment"] = "insufficient_evidence"
			item["score"] = nil
			item["comment"] = "有效证据不足，暂不评估该维度。"
			item["impact_on_deal"] = nil
			item["highlight_narrative"] = nil
		}
		if item["assessment"] == "insufficient_evidence" {
			item["comment"] = "有效证据不足，暂不评估该维度。"
			item["impact_on_deal"] = nil
			item["highlight_narrative"] = nil
		}
		// Partial coverage cannot support an absence-based score. The intermediate
		// protocol has no presence/absence marker; conservatively withhold scores
		// until that distinction can be evaluated against the full recording.
		if item["assessment"] == "scored" && (c.context["coverage"] != "complete" || c.context["truncated"] == true) {
			c.note(path, "incomplete_coverage_score_withheld")
			item["assessment"] = "insufficient_evidence"
			item["score"] = nil
			item["comment"] = "录音覆盖不完整，保留原文供复核，暂不评分。"
			item["impact_on_deal"] = nil
			item["highlight_narrative"] = nil
		}
		out[name] = item
	}
	return object{"standard_version": q["standard_version"], "dimensions": out}
}

func (c *compiler) compileCoach(hasRefund bool) object {
	coach := obj(c.model["coach"])
	practices := []any{}
	for i, raw := range arr(coach["successful_practices"]) {
		r := obj(raw)
		p := fmt.Sprintf("$.coach.successful_practices[%d]", i)
		refs := c.refs(r, "source_refs", p, "transcript")
		if len(refs) == 0 {
			c.missing(p)
			continue
		}
		practices = append(practices, object{"behavior": r["behavior"], "customer_response": r["customer_response"], "reusable_when": r["reusable_when"], "evidence_refs": list(refs)})
	}
	out := object{"successful_practices": practices, "improvement": nil, "refund_review": nil}
	if coach["improvement"] != nil {
		r := obj(coach["improvement"])
		refs := c.refs(r, "source_refs", "$.coach.improvement", "transcript")
		if len(refs) == 0 {
			c.missing("$.coach.improvement")
		} else {
			out["improvement"] = object{"suggestion": r["suggestion"], "evidence_refs": list(refs)}
		}
	}
	if coach["refund_review"] != nil {
		r := obj(coach["refund_review"])
		refs := c.refs(r, "source_refs", "$.coach.refund_review", "")
		reason := r["known_reason"]
		// Payment/refund event records carry occurrence, never causal evidence.
		if reason != nil && len(c.transcriptOnly(refs)) == 0 {
			reason = nil
			c.note("$.coach.refund_review.known_reason", "refund_fact_not_causal_evidence")
		}
		if len(refs) == 0 {
			c.missing("$.coach.refund_review")
		} else {
			out["refund_review"] = object{"known_reason": reason, "unknowns": r["unknowns"], "evidence_refs": list(refs)}
		}
	} else if hasRefund {
		out["refund_review"] = object{"known_reason": nil, "unknowns": []any{"退款事件已核实，退款原因和净额仍待核实。"}, "evidence_refs": []any{}}
	}
	return out
}
