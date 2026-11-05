package v210

import "fmt"

func (c *compiler) personalEligible() bool {
	return c.context["target_binding"] == "single_customer" && c.context["identity_confirmed"] == true && c.context["target_customer_id"] != nil && c.context["binding_record_id"] != nil
}
func (c *compiler) allBound(refs []string, binding string) bool {
	if len(refs) == 0 {
		return false
	}
	for _, ref := range refs {
		s := c.sources[ref]
		if !s.valid || s.kind != "transcript" || s.row["subject_binding"] != binding || s.row["speaker_id"] == nil || s.row["binding_record_id"] == nil {
			return false
		}
	}
	return true
}
func (c *compiler) localObservations() []any {
	entries := []publishedObservation{}
	add := func(kind string, row object, summary any, target any, path string) {
		refs := c.refs(row, "source_refs", path, "transcript")
		if len(refs) == 0 {
			return
		}
		binding := "unresolved"
		if c.allBound(refs, "target_customer") {
			binding = "target_customer"
		}
		text, _ := summary.(string)
		entry := publishedObservation{
			row:     object{"kind": kind, "summary": summary, "target": target, "subject_binding": binding, "evidence_refs": list(refs)},
			path:    path,
			kind:    kind,
			summary: text,
		}
		// rc12 drops a republished customer excerpt (evaluation id 19 published the
		// same quote as need/concern across two schemes). Template or state
		// summaries are never de-duplicated: several schemes may legitimately be
		// "暂缓" and a template must not disappear.
		switch kind {
		case "need", "concern", "trust":
			entry.dedupeKey = "statement|" + rc12NormalizeSpaces(text)
		}
		entries = append(entries, entry)
	}
	decisions := append([]any{c.model["decision"]}, arr(c.model["additional_decisions"])...)
	for j, raw := range decisions {
		d := obj(raw)
		path := "$.decision"
		if j > 0 {
			path = fmt.Sprintf("$.additional_decisions[%d]", j-1)
		}
		if c.rc11OrLater() {
			intentRefs := c.refs(d, "source_refs", path, "transcript")
			state := str(d["state"])
			if len(intentRefs) > 0 && state != "无法判断" && !c.customerSpoken(intentRefs) {
				// An employee/unknown-speaker statement is not a customer
				// decision. Move the information to a staff claim observation
				// instead of publishing it as kind=intent.
				c.note(path, "intent_not_customer_bound")
				add("staff_claim", object{"source_refs": list(intentRefs)}, rc11StaffClaimTemplate, d["target"], path)
			} else {
				add("intent", d, d["state"], d["target"], path)
			}
		} else {
			add("intent", d, d["state"], d["target"], path)
		}
		if d["condition"] != nil {
			r := obj(d["condition"])
			add("condition", r, r["summary"], d["target"], path+".condition")
		}
		n := obj(d["need"])
		if text, refs, ok := c.supportedStatement(n, c.refs(n, "source_refs", path+".need", "transcript"), path+".need", "customer_need"); ok {
			add("need", object{"source_refs": list(refs)}, text, d["target"], path+".need")
		}
		for _, k := range []string{"concerns", "trust"} {
			for i, raw := range arr(d[k]) {
				r := obj(raw)
				kind := "concern"
				if k == "trust" {
					kind = "trust"
				}
				p := fmt.Sprintf("%s.%s[%d]", path, k, i)
				allowed := []string{"customer_feeling", "customer_constraint"}
				if k == "trust" {
					allowed = []string{"customer_trust"}
				}
				if text, refs, ok := c.supportedStatement(r, c.refs(r, "source_refs", p, "transcript"), p, allowed...); ok {
					add(kind, object{"source_refs": list(refs)}, text, d["target"], p)
				}
			}
		}
	}
	risk := obj(c.model["clinical_risk"])
	if risk["summary"] != nil {
		add("clinical_statement", risk, "录音中存在待专业核实的陈述，内容以所附来源原文为准。", nil, "$.clinical_risk")
	}
	if c.rc12() {
		// Partial/multi-subject coverage must not degrade to a template-only card:
		// model-declared verbatim fragments (timing, willingness, exam) are
		// appended and the repeated excerpts are removed.
		entries = append(entries, c.localObservationEntries()...)
		entries = c.dedupePublished(entries)
	}
	out := make([]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.row)
	}
	return out
}

func (c *compiler) validateOwnership() error {
	if c.context["target_binding"] == "single_customer" && !c.personalEligible() {
		return fmt.Errorf("single target requires confirmed identity and binding record")
	}
	if c.context["target_binding"] != "single_customer" && (c.context["target_customer_id"] != nil || c.context["binding_record_id"] != nil) {
		return fmt.Errorf("unresolved or multiple targets cannot claim a single identity")
	}
	for id, s := range c.sources {
		if s.kind != "transcript" {
			continue
		}
		r := s.row
		b := r["subject_binding"]
		bound := b == "target_customer" || b == "consultant" || b == "other_person"
		if bound && (r["speaker_id"] == nil || r["binding_record_id"] == nil) {
			return fmt.Errorf("source %s bound without server identity evidence", id)
		}
		if b == "target_customer" && !c.personalEligible() {
			return fmt.Errorf("source %s claims unresolved target", id)
		}
		if b == "target_customer" && r["speaker"] != "客户" {
			return fmt.Errorf("source %s target role mismatch", id)
		}
		if b == "consultant" && r["speaker"] != "咨询师" {
			return fmt.Errorf("source %s consultant role mismatch", id)
		}
		if (b == "mixed" || b == "unresolved") && (r["speaker_id"] != nil || r["binding_record_id"] != nil) {
			return fmt.Errorf("source %s mixed identity asserted", id)
		}
	}
	return nil
}
