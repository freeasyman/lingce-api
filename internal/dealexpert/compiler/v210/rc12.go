package v210

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// rc12MaxStatementRunes is the hard display cap for one published statement.
// rc11 capped each source excerpt to 240 runes, so a statement assembled from two
// sources could still exceed the limit (evaluation id 19 published a 257-rune
// concern made of two concatenated customer quotes). rc12 caps the concatenated
// text and drops duplicated excerpts instead of republishing the same quote.
const rc12MaxStatementRunes = 240

// rc12MaxLocalObservations bounds how many model-declared local observations are
// published, so a very long multi-speaker recording cannot inflate the card.
// rc12.1 raised it and keeps timing/willingness/exam fragments first: the rc12
// real batch dropped examination items on a 488-segment recording at 16.
const (
	rc12MaxLocalObservations   = 16
	rc12P1MaxLocalObservations = 40
)

// rc12LocalKindPriority orders fragments when a very long recording exceeds the
// publish budget. Timing/willingness/exam are the rc12 defect classes, so generic
// staff chatter is dropped before them.
var rc12LocalKindPriority = map[string]int{
	"timing": 0, "willingness": 1, "exam": 2, "symptom": 3, "question": 4, "staff_statement": 5, "other": 6,
}

// rc12LocalObservationKinds is the closed set of recording-level observation
// kinds a model may declare. They are published verbatim and never carry a
// diagnostic conclusion.
var rc12LocalObservationKinds = map[string]bool{
	"timing":          true,
	"willingness":     true,
	"exam":            true,
	"symptom":         true,
	"question":        true,
	"staff_statement": true,
	"other":           true,
}

// rc12TimingKinds keeps the customer's original wording while giving the card a
// structured timing slot. The server never converts "next month" into a date.
var rc12TimingKinds = map[string]bool{
	"absolute_date":   true,
	"relative_window": true,
	"sequence":        true,
}

// rc12 and rc12.1 share the same behavioral gates; rc12.1 only repairs the card
// schema enum and the local-observation budget/prompt.
func (c *compiler) rc12() bool {
	return c.rulesVersion == RulesVersionRC12 || c.rulesVersion == RulesVersionRC12P1
}

func (c *compiler) rc12p1() bool { return c.rulesVersion == RulesVersionRC12P1 }

// rc11OrLater reports whether the rc11 guarantees (intent attribution, bounded
// statement excerpts, non-business label) apply. rc12 is a superset of rc11, so
// its additions never weaken the earlier guarantees.
func (c *compiler) rc11OrLater() bool {
	return c.rulesVersion == RulesVersionRC11 || c.rc12()
}

// publishedObservation tracks one recording observation while the card is built,
// so rc12 can de-duplicate identical excerpts across schemes without changing the
// rc11 output order.
type publishedObservation struct {
	row      object
	path     string
	kind     string
	summary  string
	dedupeKey string
}

// statementFits reports whether another verbatim excerpt still fits inside the
// rc12 total display budget. The first excerpt is already bounded by
// publishStatement; a second one may only be added when the whole display text,
// including the newline separator, stays within rc12MaxStatementRunes.
func statementFits(total int, next string) bool {
	if total == 0 {
		return true
	}
	return total+1+len([]rune(next)) <= rc12MaxStatementRunes
}

// dedupePublished drops a recording observation whose excerpt was already
// published for another scheme. The information stays available through the
// first observation and its evidence, so this removes repetition, not content.
func (c *compiler) dedupePublished(entries []publishedObservation) []publishedObservation {
	seen := map[string]string{}
	out := make([]publishedObservation, 0, len(entries))
	for _, entry := range entries {
		if entry.dedupeKey == "" {
			out = append(out, entry)
			continue
		}
		if _, ok := seen[entry.dedupeKey]; ok {
			c.note(entry.path, "duplicate_statement_dropped")
			continue
		}
		seen[entry.dedupeKey] = entry.path
		out = append(out, entry)
	}
	return out
}

// rc12OptionalArr tolerates an absent optional array; the required arrays use arr.
func rc12OptionalArr(v any) []any {
	if rows, ok := v.([]any); ok {
		return rows
	}
	return nil
}

func rc12NormalizeSpaces(text string) string {
	var b strings.Builder
	for _, r := range text {
		if !unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// verbatimLocalSummary only publishes text that exists in a cited source. A
// paraphrased summary is replaced by the source excerpt, so the model cannot
// coarsen the customer's wording ("不想做" must not become "不想查") and the
// reviewer can always locate the original sentence.
func (c *compiler) verbatimLocalSummary(row object, refs []string, path string) (string, bool) {
	summary := strings.TrimSpace(str(row["summary"]))
	if summary == "" {
		c.note(path, "missing_local_observation_summary")
		return "", false
	}
	if len([]rune(summary)) > rc12MaxStatementRunes {
		c.note(path, "local_observation_truncated")
		summary = strings.TrimSpace(string([]rune(summary)[:rc12MaxStatementRunes])) + "…"
	}
	normalized := rc12NormalizeSpaces(summary)
	normalized = strings.TrimSuffix(strings.TrimPrefix(normalized, "…"), "…")
	if normalized != "" {
		for _, ref := range refs {
			src := c.sources[ref]
			if !src.valid {
				continue
			}
			if strings.Contains(rc12NormalizeSpaces(str(src.row["text"])), normalized) {
				return summary, true
			}
		}
	}
	c.note(path, "local_observation_not_verbatim")
	src := c.sources[refs[0]]
	return rc11BoundedExcerpt([]rune(str(src.row["text"])), -1, -1, rc12MaxStatementRunes), true
}

// localObservationEntries compiles the rc12 local_observations channel. These are
// recording-level fragments with a verbatim source quote; they stay published
// even when the customer identity is unresolved, because "partial coverage" must
// not degrade a recording to a template-only card.
func (c *compiler) localObservationEntries() []publishedObservation {
	type candidate struct {
		entry   publishedObservation
		index   int
		kind    string
	}
	candidates := []candidate{}
	for i, raw := range rc12OptionalArr(c.model["local_observations"]) {
		row := obj(raw)
		path := fmt.Sprintf("$.local_observations[%d]", i)
		kind := str(row["kind"])
		if !rc12LocalObservationKinds[kind] {
			c.note(path+".kind", "invalid_local_observation_kind")
			continue
		}
		refs := c.refs(row, "source_refs", path, "transcript")
		if len(refs) == 0 {
			c.missing(path)
			continue
		}
		summary, ok := c.verbatimLocalSummary(row, refs, path)
		if !ok {
			continue
		}
		binding := "unresolved"
		if c.allBound(refs, "target_customer") {
			binding = "target_customer"
		}
		obs := object{
			"kind":            kind,
			"summary":         summary,
			"target":          nil,
			"subject_binding": binding,
			"evidence_refs":   list(refs),
		}
		if kind == "timing" {
			timingKind := str(row["timing_kind"])
			if timingKind != "" && !rc12TimingKinds[timingKind] {
				c.note(path+".timing_kind", "invalid_timing_kind")
				timingKind = ""
			}
			if timingKind == "" {
				timingKind = "unknown"
			}
			// The date stays null: the model never converts a spoken window into a
			// date, and the server does not parse one either.
			obs["timing"] = object{"kind": timingKind, "date": nil}
		}
		candidates = append(candidates, candidate{
			entry: publishedObservation{
				row:       obs,
				path:      path,
				kind:      kind,
				summary:   summary,
				dedupeKey: "local|" + kind + "|" + rc12NormalizeSpaces(summary),
			},
			index: i,
			kind:  kind,
		})
	}
	limit := rc12MaxLocalObservations
	keep := map[int]bool{}
	if c.rc12p1() {
		limit = rc12P1MaxLocalObservations
		order := make([]int, len(candidates))
		for i := range candidates {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool {
			pa, pb := rc12LocalKindPriority[candidates[order[a]].kind], rc12LocalKindPriority[candidates[order[b]].kind]
			if pa != pb {
				return pa < pb
			}
			return candidates[order[a]].index < candidates[order[b]].index
		})
		for rank, pos := range order {
			if rank >= limit {
				c.note(candidates[pos].entry.path, "local_observations_truncated")
				continue
			}
			keep[pos] = true
		}
	} else {
		for i := range candidates {
			if i >= limit {
				c.note(candidates[i].entry.path, "local_observations_truncated")
				continue
			}
			keep[i] = true
		}
	}
	out := make([]publishedObservation, 0, len(candidates))
	for i, item := range candidates {
		if keep[i] {
			out = append(out, item.entry)
		}
	}
	return out
}
