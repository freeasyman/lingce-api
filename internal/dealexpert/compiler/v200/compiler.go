package v200

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const RulesVersion = "deal-compiler-v2.0.0"

type Diagnostic struct {
	Path     string `json:"path"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
}

type Result struct {
	Status      string          `json:"status"`
	Card        json.RawMessage `json:"card"`
	Diagnostics []Diagnostic    `json:"diagnostics"`
}

type Error struct {
	Stage string
	Err   error
}

func (e *Error) Error() string { return e.Stage + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

type source struct {
	row   object
	kind  string
	valid bool
}
type compiler struct {
	input       object
	model       object
	context     object
	sources     map[string]source
	order       []string
	diagnostics []Diagnostic
	review      bool
}

// Compile owns decoded copies of the byte inputs. Local binding errors produce
// diagnostics and conservative degradation. Only invalid envelopes, structures,
// unsupported rules or an internal output invariant return an error.
func Compile(snapshot, intermediate []byte, rulesVersion string) (Result, error) {
	var empty Result
	if rulesVersion != RulesVersion {
		return empty, &Error{"rules", fmt.Errorf("unsupported rules version %q", rulesVersion)}
	}
	if err := loadSchemas(); err != nil {
		return empty, &Error{"schema_config", err}
	}
	env, err := decode(snapshot)
	if err != nil {
		return empty, &Error{"input_structure", err}
	}
	if err = schemas[0].Validate(env); err != nil {
		return empty, &Error{"input_structure", err}
	}
	m, err := decode(intermediate)
	if err != nil {
		return empty, &Error{"intermediate_structure", err}
	}
	if err = schemas[1].Validate(m); err != nil {
		return empty, &Error{"intermediate_structure", err}
	}
	e, model := obj(env), obj(m)
	in := obj(e["input"])
	hash, err := canonicalSnapshotID(in)
	if err != nil {
		return empty, &Error{"input_binding", err}
	}
	if e["input_snapshot_id"] != hash || model["input_snapshot_id"] != hash {
		return empty, &Error{"input_binding", fmt.Errorf("snapshot mismatch")}
	}
	c := &compiler{input: in, model: model, context: obj(in["context"]), sources: map[string]source{}, diagnostics: []Diagnostic{}}
	if err = c.index(); err != nil {
		return empty, &Error{"input_binding", err}
	}
	c.checkAllReferences(model, "$")
	card := c.build(hash)
	c.finishEvidence(card)
	status := "analyzed"
	if c.review {
		status = "needs_review"
	}
	card["status"] = status
	raw, err := json.Marshal(card)
	if err != nil {
		return empty, &Error{"compile_output", err}
	}
	if err = ValidateCard(raw); err != nil {
		return empty, &Error{"compile_output", err}
	}
	sort.Slice(c.diagnostics, func(i, j int) bool {
		a, b := c.diagnostics[i], c.diagnostics[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Code < b.Code
	})
	return Result{status, raw, c.diagnostics}, nil
}

func (c *compiler) note(path, code string) {
	for _, d := range c.diagnostics {
		if d.Path == path && d.Code == code {
			return
		}
	}
	c.diagnostics = append(c.diagnostics, Diagnostic{path, code, "degraded"})
}
func usable(text string) bool {
	if strings.Contains(text, "[转写过长，已截断]") {
		return false
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}
func (c *compiler) index() error {
	if c.context["truncated"] == true && c.context["coverage"] == "complete" {
		return fmt.Errorf("truncated snapshot cannot have complete coverage")
	}
	for _, set := range []struct{ key, kind string }{{"transcript_sources", "transcript"}, {"business_facts", "business_fact"}} {
		for i, raw := range arr(c.input[set.key]) {
			row := obj(raw)
			id := str(row["id"])
			if _, ok := c.sources[id]; ok {
				return fmt.Errorf("duplicate source %s", id)
			}
			valid := usable(str(row["text"]))
			path := fmt.Sprintf("$.input.%s[%d]", set.key, i)
			if !valid {
				c.note(path, "noise_or_control_source")
			}
			if set.kind == "transcript" {
				if (row["start_ms"] == nil) != (row["timing_basis"] == "unknown") {
					return fmt.Errorf("inconsistent time metadata %s", id)
				}
				if (row["speaker"] == "未知") != (row["role_basis"] == "unknown") {
					return fmt.Errorf("inconsistent role metadata %s", id)
				}
			}
			c.sources[id] = source{row, set.kind, valid}
			c.order = append(c.order, id)
		}
	}
	seenProducts := map[string]bool{}
	for _, raw := range arr(c.input["product_catalog"]) {
		key := fmt.Sprint(obj(raw)["id"])
		if seenProducts[key] {
			return fmt.Errorf("duplicate catalog id %s", key)
		}
		seenProducts[key] = true
	}
	if c.context["coverage"] != "complete" || c.context["truncated"] == true {
		c.note("$.input.context", "incomplete_coverage")
	}
	return nil
}

func (c *compiler) refs(row object, key, path, kind string) []string {
	wanted := map[string]bool{}
	for i, ref := range stringsAt(row[key]) {
		p := fmt.Sprintf("%s.%s[%d]", path, key, i)
		s, ok := c.sources[ref]
		if !ok {
			c.note(p, "unknown_source")
			continue
		}
		if kind != "" && s.kind != kind {
			c.note(p, "wrong_source_kind")
			continue
		}
		if !s.valid {
			c.note(p, "noise_source")
			continue
		}
		wanted[ref] = true
	}
	out := []string{}
	for _, ref := range c.order {
		if wanted[ref] {
			out = append(out, ref)
		}
	}
	return out
}
func (c *compiler) missing(path string) { c.note(path, "missing_evidence") }
func (c *compiler) hasRole(refs []string, role string) bool {
	for _, ref := range refs {
		s := c.sources[ref]
		if s.valid && s.kind == "transcript" && s.row["speaker"] == role && s.row["role_basis"] != "unknown" {
			return true
		}
	}
	return false
}
func (c *compiler) transcriptOnly(refs []string) []string {
	out := []string{}
	for _, ref := range refs {
		if c.sources[ref].kind == "transcript" {
			out = append(out, ref)
		}
	}
	return out
}
func (c *compiler) texts(refs []string) any {
	if len(refs) == 0 {
		return nil
	}
	out := []string{}
	for _, ref := range refs {
		out = append(out, str(c.sources[ref].row["text"]))
	}
	return strings.Join(out, "\n")
}

func (c *compiler) build(hash string) object {
	m := c.model
	d := obj(m["decision"])
	decision := object{"decision_state": d["state"], "state_basis": "unknown", "evidence_refs": []any{}, "condition": nil, "condition_due_quote": nil, "condition_evidence_refs": []any{}, "trust": []any{}, "need_state": "无法判断", "need_summary": nil, "need_evidence_refs": []any{}, "concerns": []any{}, "outcome_signals": []any{}}
	core := c.refs(d, "source_refs", "$.decision", "")
	state := str(d["state"])
	eventForState := map[string]string{"已预约": "预约", "已交定金": "定金", "已付款": "付款"}
	if eventType := eventForState[state]; eventType != "" {
		matching := []string{}
		for _, ref := range c.order {
			s := c.sources[ref]
			if s.valid && s.kind == "business_fact" && s.row["type"] == eventType {
				matching = append(matching, ref)
			}
		}
		if len(matching) > 0 {
			core = matching
			decision["state_basis"] = "verified_fact"
		} else {
			for _, ref := range core {
				if c.sources[ref].kind == "business_fact" {
					c.note("$.decision.source_refs", "wrong_fact_type")
				}
			}
			core = c.transcriptOnly(core)
			if len(core) > 0 {
				decision["state_basis"] = "reported_only"
				c.note("$.decision", "unverified_outcome")
			}
		}
	} else {
		for _, ref := range core {
			if c.sources[ref].kind != "transcript" {
				c.note("$.decision.source_refs", "wrong_source_kind")
			}
		}
		core = c.transcriptOnly(core)
		if len(core) > 0 {
			decision["state_basis"] = "transcript_judgment"
		}
	}
	if state == "无法判断" || len(core) == 0 {
		state = "无法判断"
		decision["state_basis"] = "unknown"
		core = []string{}
		c.review = true
		c.missing("$.decision")
	}
	if state == "暂缓" {
		if d["condition"] == nil {
			c.note("$.decision.condition", "missing_condition")
			c.review = true
			state = "无法判断"
			core = []string{}
			decision["state_basis"] = "unknown"
		} else {
			cond := obj(d["condition"])
			refs := c.refs(cond, "source_refs", "$.decision.condition", "transcript")
			if len(refs) == 0 {
				c.missing("$.decision.condition")
				c.review = true
				state = "无法判断"
				core = []string{}
				decision["state_basis"] = "unknown"
			} else {
				decision["condition"] = cond["summary"]
				decision["condition_due_quote"] = c.texts(refs)
				decision["condition_evidence_refs"] = list(refs)
			}
		}
	} else if d["condition"] != nil {
		c.note("$.decision.condition", "irrelevant_condition_removed")
	}
	decision["decision_state"] = state
	decision["evidence_refs"] = list(core)
	c.compileObservations(decision, d)
	clinical, policy := c.restrictions()
	facts := []any{}
	hasRefund := false
	hasPayment := false
	factRefs := []string{}
	for _, ref := range c.order {
		s := c.sources[ref]
		if s.kind != "business_fact" || !s.valid {
			continue
		}
		r := s.row
		facts = append(facts, object{"type": r["type"], "summary": r["text"], "source_record_id": r["source_record_id"], "occurred_at": r["occurred_at"], "evidence_refs": list([]string{ref})})
		factRefs = append(factRefs, ref)
		hasRefund = hasRefund || r["type"] == "退款"
		hasPayment = hasPayment || r["type"] == "付款" || r["type"] == "定金"
	}
	// The snapshot has no payment/refund amounts or supersession relation. Never
	// infer a net deal or a renewed sales mandate from coexisting events.
	if hasRefund {
		c.review = true
		c.note("$.business_facts", "refund_requires_review")
	}
	if m["confidence"] == "low" {
		c.review = true
		c.note("$.confidence", "low_confidence")
	}
	if c.review && policy != "stop" && policy != "pause_clinical" {
		policy = "review"
	}
	card := object{"schema_version": "deal-card-v2", "rules_version": RulesVersion, "input_snapshot_id": hash, "status": "analyzed", "decision": decision, "clinical_conflict": clinical, "contact_policy": policy, "business_facts": facts, "product_findings": []any{}, "primary_action": nil, "secondary_findings": []any{}, "evidence_refs": []any{}, "quality": c.compileQuality(), "coach": c.compileCoach(hasRefund), "confidence": m["confidence"], "unknowns": m["unknowns"]}
	card["contact_evidence_refs"] = list(c.refs(obj(m["contact_request"]), "source_refs", "$.contact_request", "transcript"))
	card["product_findings"] = c.compileProducts(clinical["active"] == true, policy)
	card["secondary_findings"] = c.observations(arr(m["secondary_findings"]), "$.secondary_findings")
	card["primary_action"] = c.compileAction(policy, state)
	impact := "无法判断"
	if !c.review {
		if hasPayment && !hasRefund {
			impact = "已成交可复盘"
		} else if state == "明确不考虑" {
			impact = "已流失"
		} else if decision["state_basis"] != "reported_only" {
			impact = "仍可推进"
		}
	}
	summary := "当前录音判断：" + state + "。"
	if decision["state_basis"] == "reported_only" {
		summary = "录音口述状态：" + state + "，尚未获系统事实核实。"
	}
	if len(facts) > 0 {
		types := []string{}
		for _, raw := range facts {
			types = append(types, str(obj(raw)["type"]))
		}
		summary += "系统已核实事件：" + strings.Join(types, "、") + "。"
	}
	if c.review {
		summary += "关键事项待人工复核。"
	}
	conclusion := obj(m["conclusion"])
	if conclusion["summary"] != nil {
		c.refs(conclusion, "source_refs", "$.conclusion", "")
		c.note("$.conclusion.summary", "server_summary_generated")
	}
	basis := append(append([]string{}, core...), factRefs...)
	verification := "核对客户当前意向、原文依据和已核实业务事件。"
	if c.review {
		verification = "请人工核对关键判断、来源缺口和业务事实后再决定后续处理。"
	} else if policy == "stop" {
		verification = "联系限制生效，请内部核对限制依据和处理记录。"
	} else if policy == "pause_clinical" {
		verification = "请专业人员先核对未解决事项，确认前暂停销售触达。"
	} else if policy == "review" {
		verification = "请先核对客户身份与联系授权，再决定后续处理。"
	}
	card["conclusion"] = object{"current_state": summary, "business_impact": impact, "main_basis": list(basis), "verification": verification}
	next := "当前没有可执行动作，先核对证据与业务条件。"
	if policy == "stop" {
		next = "停止联系客户，由内部核对并处理联系限制。"
	} else if c.review {
		next = "先由人工复核关键事项，再决定后续处理。"
	} else if card["primary_action"] != nil {
		a := obj(card["primary_action"])
		if a["type"] == "sales_follow_up" {
			next = "按已核验条件安排跟进，尊重客户的联系窗口。"
		} else {
			next = "先完成内部核对或专业协调，再判断后续安排。"
		}
	} else if policy == "pause_clinical" {
		next = "先由专业人员确认未解决事项，暂停销售触达。"
	} else if policy == "review" {
		next = "先核验身份和联系授权，暂不安排销售触达。"
	}
	card["brief"] = object{"situation": summary, "blocker": nil, "next_step": next, "evidence_refs": list(basis)}
	return card
}
