package v210

import (
	"fmt"
	"sort"
)

type evidenceUse struct {
	parent           object
	key, path, usage string
	refs             []string
}

// An explicit inventory is safer than interpreting every string array as a
// reference. It also makes nested references and used_for auditable.
func evidenceUses(card object) []evidenceUse {
	out := []evidenceUse{}
	add := func(parent object, key, path, usage string) {
		out = append(out, evidenceUse{parent, key, path, usage, stringsAt(parent[key])})
	}
	add(obj(card["intent"]), "evidence_refs", "$.intent.evidence_refs", "结论")
	for i, raw := range arr(card["event_reports"]) {
		add(obj(raw), "evidence_refs", fmt.Sprintf("$.event_reports[%d].evidence_refs", i), "结论")
	}
	add(obj(card["conclusion"]), "main_basis", "$.conclusion.main_basis", "结论")
	decisions := append([]any{card["decision"]}, arr(card["scheme_decisions"])...)
	for j, raw := range decisions {
		path := "$.decision"
		if j > 0 {
			path = fmt.Sprintf("$.scheme_decisions[%d]", j-1)
		}
		d := obj(raw)
		for _, key := range []string{"evidence_refs", "condition_evidence_refs"} {
			add(d, key, path+"."+key, "结论")
		}
		add(d, "need_evidence_refs", path+".need_evidence_refs", "需求")
		for _, set := range []struct{ key, usage string }{{"trust", "信任"}, {"concerns", "顾虑"}, {"outcome_signals", "结论"}} {
			for i, raw := range arr(d[set.key]) {
				add(obj(raw), "evidence_refs", fmt.Sprintf("%s.%s[%d].evidence_refs", path, set.key, i), set.usage)
			}
		}
	}
	add(obj(card["clinical_conflict"]), "evidence_refs", "$.clinical_conflict.evidence_refs", "医疗协作")
	if scope, ok := card["analysis_scope"].(object); ok {
		add(scope, "evidence_refs", "$.analysis_scope.evidence_refs", "结论")
	}
	add(card, "contact_evidence_refs", "$.contact_evidence_refs", "结论")
	for _, set := range []struct{ key, usage string }{{"product_findings", "产品"}, {"secondary_findings", "结论"}, {"business_facts", "结论"}} {
		for i, raw := range arr(card[set.key]) {
			add(obj(raw), "evidence_refs", fmt.Sprintf("$.%s[%d].evidence_refs", set.key, i), set.usage)
		}
	}
	if card["primary_action"] != nil {
		a := obj(card["primary_action"])
		add(a, "source_evidence_refs", "$.primary_action.source_evidence_refs", "动作")
		add(a, "timing_evidence_refs", "$.primary_action.timing_evidence_refs", "动作")
		if a["customer_window"] != nil {
			add(obj(a["customer_window"]), "evidence_refs", "$.primary_action.customer_window.evidence_refs", "动作")
		}
	}
	q := obj(obj(card["quality"])["dimensions"])
	for _, name := range qualityNames {
		add(obj(q[name]), "evidence_refs", "$.quality.dimensions."+name+".evidence_refs", "质检")
	}
	coach := obj(card["coach"])
	for i, raw := range arr(coach["observations"]) {
		add(obj(raw), "evidence_refs", fmt.Sprintf("$.coach.observations[%d].evidence_refs", i), "复盘")
		add(obj(raw), "response_evidence_refs", fmt.Sprintf("$.coach.observations[%d].response_evidence_refs", i), "复盘")
	}
	for _, key := range []string{"improvement"} {
		if coach[key] != nil {
			add(obj(coach[key]), "evidence_refs", "$.coach."+key+".evidence_refs", "复盘")
		}
	}
	for i, raw := range arr(card["recording_observations"]) {
		add(obj(raw), "evidence_refs", fmt.Sprintf("$.recording_observations[%d].evidence_refs", i), "结论")
	}
	add(obj(card["brief"]), "evidence_refs", "$.brief.evidence_refs", "结论")
	return out
}

func (c *compiler) finishEvidence(card object) {
	uses := evidenceUses(card)
	usages := map[string]map[string]bool{}
	for _, use := range uses {
		for _, ref := range use.refs {
			if usages[ref] == nil {
				usages[ref] = map[string]bool{}
			}
			usages[ref][use.usage] = true
		}
	}
	// Allocate over all usable snapshot sources, independent of model ordering
	// and which optional observations survive. Unused ids may leave gaps.
	ids := map[string]string{}
	n := 0
	for _, ref := range c.order {
		if c.sources[ref].valid {
			n++
			ids[ref] = fmt.Sprintf("ev_%d", n)
		}
	}
	evidence := []any{}
	for _, ref := range c.order {
		if len(usages[ref]) == 0 {
			continue
		}
		s := c.sources[ref]
		r := s.row
		used := []string{}
		for u := range usages[ref] {
			used = append(used, u)
		}
		sort.Strings(used)
		e := object{"id": ids[ref], "quote": r["text"], "speaker": "系统事实", "source_ref": ref, "source_kind": s.kind, "start_ms": nil, "timing_basis": "unknown", "role_basis": "system_verified", "used_for": list(used)}
		if s.kind == "transcript" {
			e["speaker"] = r["speaker"]
			e["start_ms"] = r["start_ms"]
			e["timing_basis"] = r["timing_basis"]
			e["role_basis"] = r["role_basis"]
		}
		evidence = append(evidence, e)
	}
	for _, use := range uses {
		wanted := map[string]bool{}
		for _, ref := range use.refs {
			wanted[ref] = true
		}
		refs := []string{}
		for _, ref := range c.order {
			if wanted[ref] {
				refs = append(refs, ids[ref])
			}
		}
		use.parent[use.key] = list(refs)
	}
	card["evidence_refs"] = evidence
}
