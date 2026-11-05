package v200

import "fmt"

func (c *compiler) restrictions() (object, string) {
	risk := obj(c.model["clinical_risk"])
	riskRefs := c.refs(risk, "source_refs", "$.clinical_risk", "transcript")
	active := c.context["clinical_hold"] == true || risk["assessment"] == "unresolved"
	reason := risk["summary"]
	assessment := risk["assessment"]
	if risk["assessment"] == "unresolved" && len(riskRefs) == 0 {
		c.review = true
		c.missing("$.clinical_risk")
		reason = "医疗限制依据待复核，确认前暂停销售触达。"
	}
	if c.context["clinical_hold"] == true {
		reason = "系统医疗暂停限制生效，待专业人员核实。"
		assessment = "unresolved"
		c.note("$.clinical_risk", "system_clinical_hold")
	}
	if !active {
		reason = nil
		if c.context["coverage"] != "complete" || c.context["truncated"] == true {
			assessment = "unknown"
		}
	}
	clinical := object{"active": active, "reason": reason, "assessment": assessment, "evidence_refs": list(riskRefs)}
	request := obj(c.model["contact_request"])
	refs := c.refs(request, "source_refs", "$.contact_request", "transcript")
	if c.context["contact_forbidden"] == true {
		c.note("$.contact_request", "system_contact_forbidden")
		return clinical, "stop"
	}
	if request["value"] == "stop" {
		if len(refs) > 0 {
			return clinical, "stop"
		}
		c.review = true
		c.missing("$.contact_request")
	}
	if active {
		return clinical, "pause_clinical"
	}
	if c.review || c.context["identity_confirmed"] != true || c.context["contact_authorized"] != true {
		if c.context["identity_confirmed"] != true {
			c.note("$.input.context.identity_confirmed", "identity_unconfirmed")
		}
		if c.context["contact_authorized"] != true {
			c.note("$.input.context.contact_authorized", "contact_not_authorized")
		}
		return clinical, "review"
	}
	return clinical, "allow"
}

func (c *compiler) compileProducts(clinical bool, policy string) []any {
	out := []any{}
	seen := map[string]bool{}
	catalog := map[string]object{}
	for _, raw := range arr(c.input["product_catalog"]) {
		r := obj(raw)
		catalog[fmt.Sprint(r["id"])] = r
	}
	for i, raw := range arr(c.model["product_findings"]) {
		r := obj(raw)
		p := fmt.Sprintf("$.product_findings[%d]", i)
		refs := c.refs(r, "source_refs", p, "transcript")
		if len(refs) == 0 {
			c.missing(p)
			continue
		}
		if clinical {
			c.note(p, "clinical_hold_product_removed")
			continue
		}
		t := str(r["finding_type"])
		if t == "可能漏推" && (c.context["allow_missed_opportunity"] != true || c.context["coverage"] != "complete" || c.context["truncated"] == true || c.context["catalog_complete"] != true) {
			c.note(p, "missed_opportunity_not_supported")
			continue
		}
		if (t == "升级机会" || t == "联合机会" || t == "替代方案") && c.context["allow_extended_opportunities"] != true {
			c.note(p, "extended_opportunity_disabled")
			continue
		}
		id := r["catalog_product_id"]
		name := r["mentioned_name"]
		var version any
		requires := true
		if id != nil {
			item, ok := catalog[fmt.Sprint(id)]
			if !ok {
				c.note(p, "unknown_catalog_product")
				continue
			}
			name = item["name"]
			version = item["version"]
			requires = item["requires_clinical_confirmation"] == true
		} else if str(name) == "" {
			c.note(p, "missing_product_identity")
			continue
		}
		key := fmt.Sprint(id) + "|" + str(name) + "|" + t
		if seen[key] {
			c.note(p, "duplicate_product_removed")
			continue
		}
		seen[key] = true
		suggested := r["suggested_action"]
		if policy != "allow" || c.review {
			suggested = nil
		}
		out = append(out, object{"product_id": id, "product_name": name, "product_version": version, "finding_type": t, "customer_evidence": r["customer_evidence_summary"], "matching_reason": r["matching_reason"], "recommended_by_consultant": r["recommended_by_consultant"], "requires_clinical_confirmation": requires, "suggested_action": suggested, "confidence": r["confidence"], "evidence_refs": list(refs)})
	}
	return out
}

func (c *compiler) compileAction(policy, state string) any {
	if c.model["proposed_action"] == nil {
		return nil
	}
	a := obj(c.model["proposed_action"])
	refs := c.refs(a, "source_refs", "$.proposed_action", "transcript")
	timing := c.refs(a, "timing_source_refs", "$.proposed_action", "transcript")
	if len(refs) == 0 {
		c.missing("$.proposed_action")
		return nil
	}
	if c.review || policy == "stop" || policy == "review" {
		c.note("$.proposed_action", "action_blocked_by_review_or_contact_policy")
		return nil
	}
	if policy == "pause_clinical" && a["type"] != "internal_coordination" {
		c.note("$.proposed_action", "clinical_only_internal_coordination")
		return nil
	}
	due := str(a["due_basis"])
	for _, ref := range timing {
		s := c.sources[ref]
		if s.row["role_basis"] == "unknown" || (s.row["speaker"] != "客户" && s.row["speaker"] != "家属") {
			c.note("$.proposed_action.timing_source_refs", "customer_window_role_unconfirmed")
			return nil
		}
	}
	if len(arr(a["timing_source_refs"])) != len(timing) {
		c.note("$.proposed_action", "invalid_timing_evidence")
		return nil
	}
	eventWindow := due == "after_family_discussion" || due == "after_clinical_confirmation"
	if eventWindow && len(timing) == 0 {
		c.note("$.proposed_action", "event_window_missing_evidence")
		return nil
	}
	if a["type"] == "sales_follow_up" {
		for _, ref := range c.order {
			s := c.sources[ref]
			if s.valid && s.kind == "business_fact" && (s.row["type"] == "付款" || s.row["type"] == "定金") {
				c.note("$.proposed_action", "verified_payment_requires_service_review")
				return nil
			}
		}
		if state == "明确不考虑" || state == "已付款" || state == "已交定金" {
			c.note("$.proposed_action", "state_disallows_sales_action")
			return nil
		}
		if state == "暂缓" && !eventWindow {
			c.note("$.proposed_action", "deferred_customer_window_required")
			return nil
		}
		decisionRefs := c.refs(obj(c.model["decision"]), "source_refs", "$.decision", "transcript")
		if !c.hasRole(decisionRefs, "客户") {
			c.note("$.proposed_action", "sales_customer_role_unconfirmed")
			return nil
		}
	}
	provenance := "system_suggestion"
	if len(timing) > 0 {
		provenance = "customer_source"
	}
	return object{"type": a["type"], "goal": a["goal"], "action": a["action"], "suggested_content": a["suggested_content"], "owner_employee_id": nil, "due_basis": due, "due_quote": c.texts(timing), "due_provenance": provenance, "timing_evidence_refs": list(timing), "source_evidence_refs": list(refs)}
}
