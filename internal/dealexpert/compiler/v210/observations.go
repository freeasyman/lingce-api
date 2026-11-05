package v210

import "fmt"

func (c *compiler) compileObservations(out, d object, path string) {
	trust := []any{}
	for i, raw := range arr(d["trust"]) {
		r := obj(raw)
		p := fmt.Sprintf("%s.trust[%d]", path, i)
		refs := c.refs(r, "source_refs", p, "transcript")
		if len(refs) == 0 || !c.allBound(refs, "target_customer") {
			c.missing(p)
			continue
		}
		summary, exact, ok := c.supportedStatement(r, refs, p, "customer_trust")
		if !ok {
			continue
		}
		trust = append(trust, object{"trust_target": r["target"], "target_label": r["target_label"], "state": r["state"], "summary": summary, "evidence_refs": list(exact)})
	}
	out["trust"] = trust
	n := obj(d["need"])
	refs := c.refs(n, "source_refs", path+".need", "transcript")
	if len(refs) > 0 && c.allBound(refs, "target_customer") {
		summary, exact, ok := c.supportedStatement(n, refs, path+".need", "customer_need")
		if ok {
			out["need_state"] = n["state"]
			out["need_summary"] = summary
			out["need_evidence_refs"] = list(exact)
		}
	} else if n["state"] != "无法判断" || n["summary"] != nil {
		c.missing(path + ".need")
	}
	concerns := []any{}
	primaryCount := 0
	for i, raw := range arr(d["concerns"]) {
		r := obj(raw)
		p := fmt.Sprintf("%s.concerns[%d]", path, i)
		refs := c.refs(r, "source_refs", p, "transcript")
		if len(refs) == 0 || !c.allBound(refs, "target_customer") {
			c.missing(p)
			continue
		}
		summary, exact, ok := c.supportedStatement(r, refs, p, "customer_feeling", "customer_constraint")
		if !ok {
			continue
		}
		if r["is_primary"] == true {
			primaryCount++
		}
		concerns = append(concerns, object{"type": r["type"], "type_label": r["type_label"], "summary": summary, "is_primary": r["is_primary"], "evidence_refs": list(exact)})
	}
	if primaryCount > 1 {
		c.review = true
		c.note(path+".concerns", "conflicting_primary_concerns")
		for _, r := range concerns {
			obj(r)["is_primary"] = false
		}
	}
	out["concerns"] = concerns
	signals := []any{}
	for i, raw := range arr(d["outcome_signals"]) {
		r := obj(raw)
		p := fmt.Sprintf("%s.outcome_signals[%d]", path, i)
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
		if item["assessment"] == "scored" && (c.context["consultant_role_verified"] != true || c.context["target_binding"] != "single_customer" || c.context["coverage"] != "complete" || c.context["truncated"] == true) {
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

var communicationTemplates = map[string]string{
	"clarify_question":      "请澄清客户当前问题和所讨论的方案。",
	"confirm_understanding": "请客户复述理解，并核对仍需解释的问题。",
	"explain_process":       "说明接待流程，并确认客户希望如何安排。",
	"request_permission":    "先征得客户对后续联系和安排的许可。",
	"professional_handoff":  "将未解决的问题及原文交给专业人员核实。",
}

func (c *compiler) compileCoach(_ bool) object {
	coach := obj(c.model["coach"])
	observations := []any{}
	for i, raw := range arr(coach["observations"]) {
		r := obj(raw)
		path := fmt.Sprintf("$.coach.observations[%d]", i)
		refs := c.refs(r, "source_refs", path, "transcript")
		response := c.refs(r, "response_source_refs", path, "transcript")
		if len(refs) == 0 {
			c.missing(path)
			continue
		}
		var text any
		// Independent, server-bound customer response is needed to report a response.
		independent := true
		for _, a := range refs {
			for _, b := range response {
				if a == b {
					independent = false
				}
			}
		}
		if independent && c.allBound(response, "target_customer") && c.allBound(refs, "consultant") {
			text = "录音中存在独立客户回应；内容见原文，不据此推断接受或疗效。"
		} else {
			response = []string{}
			c.note(path, "response_not_independently_bound")
		}
		observations = append(observations, object{"behavior": communicationTemplates[str(r["behavior"])], "customer_response": text, "evidence_refs": list(refs), "response_evidence_refs": list(response)})
	}
	out := object{"observations": observations, "improvement": nil}
	if coach["improvement"] != nil {
		r := obj(coach["improvement"])
		refs := c.refs(r, "source_refs", "$.coach.improvement", "transcript")
		if len(refs) > 0 {
			out["improvement"] = object{"suggestion": communicationTemplates[str(r["template"])], "evidence_refs": list(refs)}
		}
	}
	return out
}
