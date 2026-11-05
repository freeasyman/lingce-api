package v210

import "strings"

// rc11MaxStatementRunes is the display cap for a published customer statement.
// It is deliberately larger than every normal utterance in the current real
// batches, so only genuinely long mixed-role transcription blobs are cut.
const rc11MaxStatementRunes = 240

const (
	rc11StaffClaimTemplate    = "员工或未绑定说话人提出方案适配判断，内容以来源原文为准，待专业核实。"
	rc11NonBusinessLabel      = "非业务录音：内容为系统/界面或流程使用反馈，未涉及医疗咨询或销售沟通。"
	rc11NonBusinessConclusion = "本录音为非业务内容（系统/流程界面反馈），未涉及医疗咨询或销售沟通。"
	rc11NonBusinessNextStep   = "该录音不涉及医疗或销售沟通，无需生成销售跟进；如需要可转产品/系统反馈处理。"
	rc11NonBusinessVerify     = "非业务内容，无需核对客户意向或业务事实。"
	rc11UnsetScopeLabel       = "分析范围未声明。"
	rc11BusinessScopeLabel    = "医疗/销售咨询录音。"
	rc11UnclearScopeLabel     = "分析范围未明确。"
)

func (c *compiler) rc11() bool { return c.rulesVersion == RulesVersionRC11 }

// customerSpoken reports whether at least one cited source is a cleaned
// customer turn with a known role. It is intentionally weaker than identity
// binding: on an unresolved recording it only proves that the words were
// attributed to the customer channel, not who the customer is.
func (c *compiler) customerSpoken(refs []string) bool {
	for _, ref := range refs {
		s := c.sources[ref]
		if s.valid && s.kind == "transcript" && s.row["speaker"] == "客户" && s.row["role_basis"] != "unknown" {
			return true
		}
	}
	return false
}

// publishStatement keeps rc7-rc10 byte-identical. For rc11 it caps a long
// published statement to a sentence window around the cited span. The excerpt
// is still verbatim source text; it only drops unrelated sentences from a long
// merged transcription segment (which is how mixed-role chatter leaked into a
// customer concern in rc10).
func (c *compiler) publishStatement(raw string, start, end int, path string) string {
	if !c.rc11OrLater() {
		return raw
	}
	r := []rune(raw)
	if len(r) <= rc11MaxStatementRunes {
		return raw
	}
	c.note(path, "statement_summary_truncated")
	return rc11BoundedExcerpt(r, start, end, rc11MaxStatementRunes)
}

func rc11IsSentenceBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '!', '?', ';', '\n':
		return true
	}
	return false
}

func rc11SentenceEnd(r []rune, pos int) int {
	if pos < 0 {
		pos = 0
	}
	if pos > len(r) {
		pos = len(r)
	}
	if pos == len(r) {
		return len(r)
	}
	if pos > 0 && rc11IsSentenceBoundary(r[pos-1]) {
		return pos
	}
	for pos < len(r) && !rc11IsSentenceBoundary(r[pos]) {
		pos++
	}
	if pos < len(r) {
		pos++
	}
	return pos
}

func rc11SentenceStart(r []rune, pos int) int {
	if pos < 0 {
		return 0
	}
	if pos > len(r) {
		pos = len(r)
	}
	if pos == 0 {
		return 0
	}
	if rc11IsSentenceBoundary(r[pos-1]) {
		return pos
	}
	for pos > 0 && !rc11IsSentenceBoundary(r[pos-1]) {
		pos--
	}
	return pos
}

// rc11BoundedExcerpt selects the complete sentence that contains the span. A
// span outside the source falls back to the leading sentence, never to the whole
// blob.
func rc11BoundedExcerpt(r []rune, start, end, limit int) string {
	n := len(r)
	if n <= limit {
		return string(r)
	}
	if start < 0 || end <= start || end > n || limit <= 0 {
		start = 0
		end = rc11SentenceEnd(r, 0)
		if end <= 0 {
			end = n
		}
	}
	s := rc11SentenceStart(r, start)
	e := rc11SentenceEnd(r, end)
	if s > e {
		s, e = 0, n
	}
	if e-s > limit {
		e = s + limit
		// Prefer a punctuation boundary inside the budget.
		for i := s + limit - 1; i > s; i-- {
			if rc11IsSentenceBoundary(r[i]) {
				e = i + 1
				break
			}
		}
	}
	text := strings.TrimSpace(string(r[s:e]))
	if text == "" {
		text = strings.TrimSpace(string(r[:minInt(limit, n)]))
	}
	prefix, suffix := "", ""
	if s > 0 {
		prefix = "…"
	}
	if e < n {
		suffix = "…"
	}
	return prefix + text + suffix
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// compileScope keeps the server-owned analysis scope label. The model may only
// choose the kind and cite transcript sources; the display label is a template.
func (c *compiler) compileScope() object {
	out := object{"kind": "unclear", "label": rc11UnsetScopeLabel, "evidence_refs": []any{}}
	raw, ok := c.model["analysis_scope"].(object)
	if !ok {
		return out
	}
	kind := str(raw["kind"])
	refs := c.refs(raw, "source_refs", "$.analysis_scope", "transcript")
	switch kind {
	case "non_business":
		if len(refs) == 0 {
			c.note("$.analysis_scope", "non_business_without_source")
			return object{"kind": "unclear", "label": rc11UnclearScopeLabel, "evidence_refs": []any{}}
		}
		return object{"kind": "non_business", "label": rc11NonBusinessLabel, "evidence_refs": list(refs)}
	case "business":
		return object{"kind": "business", "label": rc11BusinessScopeLabel, "evidence_refs": list(refs)}
	case "unclear":
		return object{"kind": "unclear", "label": rc11UnclearScopeLabel, "evidence_refs": list(refs)}
	default:
		c.note("$.analysis_scope", "invalid_analysis_scope_kind")
		return out
	}
}
