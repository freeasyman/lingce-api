package v210

import (
	"encoding/json"
	"fmt"
)

// supportedStatement returns the exact frozen customer text only when the
// model's provenance is structurally tied to a customer source and the stated
// category is allowed for the field. A model summary is never trusted.
func (c *compiler) supportedStatement(row object, refs []string, path string, allowed ...string) (string, []string, bool) {
	s, _ := row["support"].(object)
	if s == nil {
		c.note(path, "missing_statement_provenance")
		return "", nil, false
	}
	kind := str(s["statement_type"])
	okKind := false
	for _, a := range allowed {
		if kind == a {
			okKind = true
		}
	}
	if !okKind || str(s["confidence"]) == "low" || str(s["confidence"]) == "unknown" {
		c.note(path, "unsupported_statement_type")
		return "", nil, false
	}
	if c.conflictingStatementTypes(s) {
		c.note(path, "conflicting_statement_types")
		return "", nil, false
	}
	spans := arr(s["spans"])
	if len(spans) == 0 {
		c.note(path, "missing_statement_span")
		return "", nil, false
	}
	texts := ""
	used := []string{}
	// rc12 counts the runes already published so a statement assembled from two
	// sources cannot exceed the display cap (rc11 only bounded each excerpt).
	total := 0
	for _, raw := range spans {
		p := obj(raw)
		ref := str(p["source_ref"])
		found := false
		for _, x := range refs {
			if x == ref {
				found = true
			}
		}
		if !found {
			c.note(path, "statement_ref_not_in_field_refs")
			return "", nil, false
		}
		src, exists := c.sources[ref]
		if !exists || !src.valid || src.kind != "transcript" || src.row["speaker"] != "客户" || src.row["subject_binding"] == "consultant" || src.row["subject_binding"] == "other_person" || src.row["subject_binding"] == "mixed" {
			c.note(path, "statement_not_customer_bound")
			return "", nil, false
		}
		start, end := intNumber(p["start"]), intNumber(p["end"])
		r := []rune(str(src.row["text"]))
		if start < 0 || end <= start || end > len(r) {
			// The selected span only locates evidence. rc7-rc10 publish the
			// entire utterance so a model cannot hide negation, conditions, or
			// attribution. rc11 keeps that intent but bounds the excerpt to the
			// span sentence (plus one adjacent sentence) when an ASR segment is
			// a long mixed-role blob. An invalid locator must not erase the
			// bounded customer utterance.
			c.note(path, "statement_span_out_of_range")
		}
		duplicate := false
		for _, old := range used {
			if old == ref {
				duplicate = true
			}
		}
		if duplicate {
			continue
		}
		excerpt := c.publishStatement(string(r), start, end, path)
		if c.rc12() && !statementFits(total, excerpt) {
			// The excerpt is dropped, not truncated: every published part stays a
			// complete verbatim slice of its source. The evidence ref is kept so the
			// reviewer can still open the omitted source.
			c.note(path, "statement_summary_total_truncated")
			used = append(used, ref)
			continue
		}
		if texts != "" {
			texts += "\n"
			total++
		}
		texts += excerpt
		total += len([]rune(excerpt))
		used = append(used, ref)
	}
	return texts, used, true
}
func intNumber(v any) int {
	switch x := v.(type) {
	case json.Number:
		n, e := x.Int64()
		if e != nil {
			return -1
		}
		return int(n)
	case int:
		return x
	case float64:
		return int(x)
	default:
		return -1
	}
}

// Identical excerpts cannot simultaneously have incompatible provenance types
// in different schemes. Do not choose the classification most useful to sales.
func (c *compiler) conflictingStatementTypes(s object) bool {
	keys := map[string]bool{}
	for _, raw := range arr(s["spans"]) {
		p := obj(raw)
		keys[fmt.Sprintf("%s/%v/%v", p["source_ref"], p["start"], p["end"])] = true
	}
	decisions := append([]any{c.model["decision"]}, arr(c.model["additional_decisions"])...)
	for _, raw := range decisions {
		d := obj(raw)
		rows := append([]any{d["need"]}, arr(d["trust"])...)
		rows = append(rows, arr(d["concerns"])...)
		for _, v := range rows {
			other, _ := obj(v)["support"].(object)
			if other == nil || !incompatibleStatementTypes(str(other["statement_type"]), str(s["statement_type"])) {
				continue
			}
			for _, r := range arr(other["spans"]) {
				p := obj(r)
				if keys[fmt.Sprintf("%s/%v/%v", p["source_ref"], p["start"], p["end"])] {
					return true
				}
			}
		}
	}
	return false
}

func incompatibleStatementTypes(a, b string) bool {
	if a == b {
		return false
	}
	// Feeling and practical constraint can overlap; a literal choice and an
	// inferred need cannot be treated as interchangeable for the same excerpt.
	if (a == "customer_choice" && b == "customer_need") || (b == "customer_choice" && a == "customer_need") {
		return true
	}
	for _, v := range []string{"customer_question", "staff_claim", "inference", "unknown"} {
		if a == v || b == v {
			return true
		}
	}
	return false
}
