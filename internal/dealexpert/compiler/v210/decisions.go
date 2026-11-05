package v210

// compileDecision binds each scheme independently. Failure in one scheme does
// not erase another scheme's supported state or condition.
func (c *compiler) compileDecision(d object, path string) object {
	out := object{"decision_state": d["state"], "state_basis": "unknown", "evidence_refs": []any{}, "condition": nil, "condition_kind": nil, "condition_due_quote": nil, "condition_evidence_refs": []any{}, "trust": []any{}, "need_state": "无法判断", "need_summary": nil, "need_evidence_refs": []any{}, "concerns": []any{}, "outcome_signals": []any{}, "target": d["target"], "confidence": object{"level": "unknown", "scope": "scheme"}}
	core := c.refs(d, "source_refs", path, "transcript")
	state := str(d["state"])
	if !c.personalEligible() || !c.allBound(core, "target_customer") || d["target"] == nil {
		c.review = true
		c.note(path, "target_binding_unresolved")
		out["decision_state"] = "无法判断"
		if c.personalEligible() {
			c.compileObservations(out, d, path)
		}
		return out
	}
	if state == "无法判断" || len(core) == 0 {
		c.review = true
		c.missing(path)
		out["decision_state"] = "无法判断"
		c.compileObservations(out, d, path)
		return out
	}
	out["evidence_refs"] = list(core)
	if cv := obj(d["confidence"]); cv["level"] != nil {
		out["confidence"] = object{"level": cv["level"], "scope": "scheme"}
	}
	out["state_basis"] = "transcript_judgment"
	if obj(out["confidence"])["level"] == "low" || obj(out["confidence"])["level"] == "unknown" {
		c.review = true
		c.note(path+".confidence", "uncertain_scheme")
	}
	if d["condition"] != nil {
		cond := obj(d["condition"])
		refs := c.refs(cond, "source_refs", path+".condition", "transcript")
		if c.allBound(refs, "target_customer") {
			out["condition"] = cond["summary"]
			out["condition_kind"] = cond["kind"]
			out["condition_due_quote"] = c.texts(refs)
			out["condition_evidence_refs"] = list(refs)
		} else {
			c.missing(path + ".condition")
			c.review = true
		}
	}
	if state == "暂缓" && out["condition"] == nil {
		c.review = true
		c.note(path+".condition", "missing_condition")
		out["decision_state"] = "无法判断"
		out["state_basis"] = "unknown"
		out["evidence_refs"] = []any{}
		out["confidence"] = object{"level": "unknown", "scope": "scheme"}
	}
	c.compileObservations(out, d, path)
	return out
}
