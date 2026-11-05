package v210

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const RulesVersion = "deal-compiler-v2.1.0-rc7"

// RulesVersionRC8 shares rc7's compiler logic but has an independent prompt,
// rules version and final card schema. It does not replace the rc7 release.
const RulesVersionRC8 = "deal-compiler-v2.1.0-rc8"

// RulesVersionRC9 keeps rc8's compiler logic byte-identical and only adds two
// prompt-level reminders (coach closed enum and timing recall). It is a separate
// rules version so an rc9 card can never be read as rc8 or rc7.
const RulesVersionRC9 = "deal-compiler-v2.1.0-rc9"

// RulesVersionRC10 keeps rc9's compiler logic byte-identical and adds two
// prompt-level semantic constraints: do not strengthen a customer's tentative
// wording, and do not classify symptoms/findings as customer needs. The
// compiler still maps the model's intermediate faithfully; the fix is at the
// model instruction step, not a keyword rule. It is a separate rules version so
// an rc10 card can never be read as rc9, rc8 or rc7.
const RulesVersionRC10 = "deal-compiler-v2.1.0-rc10"

// RulesVersionRC11 keeps rc10 frozen and adds compiler-level guarantees plus a
// small protocol addition:
//
//  1. a recording-level "intent" cannot be published from employee/unknown
//     speakers only (the rc10 batch exposed a staff medical-adaptation
//     conclusion being written as customer intent);
//  2. a strong refusal ("明确不考虑/明确拒绝") needs customer-spoken evidence;
//  3. published need/concern/trust text is a bounded sentence excerpt around the
//     cited span instead of a whole long mixed-role utterance;
//  4. the optional analysis_scope lets the model mark a recording as
//     non_business and the server keep that label in the final card.
//
// rc7-rc10 prompts, schemas and card bytes stay untouched. All new behavior is
// gated on this constant so older versions remain reproducible.
const RulesVersionRC11 = "deal-compiler-v2.1.0-rc11"

// RulesVersionRC12 keeps rc11 frozen and adds the rc12 fixes: a hard total cap on
// a published statement (rc11 bounded each source excerpt, so two concatenated
// quotes could exceed 240 runes), de-duplication of a republished excerpt across
// schemes, and an additive local_observations channel for verbatim
// timing/willingness/exam fragments on partially covered or multi-subject
// recordings. rc7-rc11 prompts, schemas and card bytes stay untouched.
const RulesVersionRC12 = "deal-compiler-v2.1.0-rc12"

// RulesVersionRC12P1 is the rc12 repair release. The first rc12 real batch (09)
// failed one record at compile_output because a model-declared kind=other local
// observation was missing from the rc12 final card schema; that control failure is
// a rc12 defect, not a model result. rc12.1 adds other to the card enum, raises the
// local-observation budget with timing/willingness/exam priority, and clarifies
// the prompt. rc12 cards and batch 09 stay frozen and reproducible.
const RulesVersionRC12P1 = "deal-compiler-v2.1.0-rc12.1"

// IsRulesVersion reports whether this package can execute the selected v2.1
// contract. Unknown versions must never fall back to the current behavior.
func IsRulesVersion(version string) bool {
	return version == RulesVersion || version == RulesVersionRC8 || version == RulesVersionRC9 || version == RulesVersionRC10 || version == RulesVersionRC11 || version == RulesVersionRC12 || version == RulesVersionRC12P1
}

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
	input        object
	model        object
	context      object
	sources      map[string]source
	order        []string
	diagnostics  []Diagnostic
	review       bool
	rulesVersion string
}

// Compile owns decoded copies of the byte inputs. Local binding errors produce
// diagnostics and conservative degradation. Only invalid envelopes, structures,
// unsupported rules or an internal output invariant return an error.
func compileSnapshot(snapshot, intermediate []byte, rulesVersion string) (Result, error) {
	var empty Result
	if !IsRulesVersion(rulesVersion) {
		return empty, &Error{"rules", fmt.Errorf("unsupported rules version %q", rulesVersion)}
	}
	set, err := schemaSet(rulesVersion)
	if err != nil {
		return empty, &Error{"schema_config", err}
	}
	env, err := decode(snapshot)
	if err != nil {
		return empty, &Error{"input_structure", err}
	}
	if err = set[0].Validate(env); err != nil {
		return empty, &Error{"input_structure", err}
	}
	m, err := decode(intermediate)
	if err != nil {
		return empty, &Error{"intermediate_structure", err}
	}
	if err = set[1].Validate(m); err != nil {
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
	c := &compiler{input: in, model: model, context: obj(in["context"]), sources: map[string]source{}, diagnostics: []Diagnostic{}, rulesVersion: rulesVersion}
	if err = c.index(); err != nil {
		return empty, &Error{"input_binding", err}
	}
	if err = c.validateOwnership(); err != nil {
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
	if err = ValidateCardForVersion(raw, rulesVersion); err != nil {
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
	decision := c.compileDecision(d, "$.decision")
	state := str(decision["decision_state"])
	core := stringsAt(decision["evidence_refs"])
	schemes := []any{decision}
	for i, raw := range arr(m["additional_decisions"]) {
		schemes = append(schemes, c.compileDecision(obj(raw), fmt.Sprintf("$.additional_decisions[%d]", i)))
	}
	clinical, policy := c.restrictions()
	facts := []any{}
	hasRefund := false
	hasPayment := false
	factRefs := []string{}
	for _, ref := range c.order {
		s := c.sources[ref]
		if s.kind != "business_fact" || !s.valid || !c.personalEligible() || fmt.Sprint(s.row["target_customer_id"]) != fmt.Sprint(c.context["target_customer_id"]) {
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
	card := object{"schema_version": "deal-card-v2.1", "rules_version": c.rulesVersion, "input_snapshot_id": hash, "status": "analyzed", "decision": decision, "clinical_conflict": clinical, "contact_policy": policy, "business_facts": facts, "product_findings": []any{}, "primary_action": nil, "secondary_findings": []any{}, "evidence_refs": []any{}, "quality": c.compileQuality(), "coach": c.compileCoach(hasRefund), "confidence": m["confidence"], "unknowns": m["unknowns"]}
	card["target_binding"] = object{"state": c.context["target_binding"], "target_customer_id": c.context["target_customer_id"], "binding_record_id": c.context["binding_record_id"]}
	scope := object{"kind": "unclear", "label": rc11UnsetScopeLabel, "evidence_refs": []any{}}
	nonBusiness := false
	if c.rc11OrLater() {
		scope = c.compileScope()
		nonBusiness = str(scope["kind"]) == "non_business"
		card["analysis_scope"] = scope
	}
	observations := c.localObservations()
	if nonBusiness {
		// The visible label must survive server summary generation and sit in the
		// same list a reviewer scans, so a noise recording is not shown as a
		// series of unlabelled "findings".
		obs := []any{object{"kind": "non_business", "summary": rc11NonBusinessLabel, "target": nil, "subject_binding": "unresolved", "evidence_refs": scope["evidence_refs"]}}
		observations = append(obs, observations...)
	}
	card["recording_observations"] = observations
	card["scheme_decisions"] = schemes
	intentRefs := c.refs(d, "source_refs", "$.decision", "transcript")
	intentState := d["state"]
	if len(intentRefs) == 0 {
		intentState = "无法判断"
	} else if c.rc11OrLater() && intentState != "无法判断" && !c.customerSpoken(intentRefs) {
		// Top-level intent is still a recording judgement. Unknown speakers
		// cannot be published as the customer's state.
		c.note("$.decision", "intent_not_customer_bound")
		intentState = "无法判断"
	}
	card["intent"] = object{"state": intentState, "target": d["target"], "scope": "recording", "evidence_refs": list(intentRefs)}
	card["event_reports"] = []any{}
	for i, raw := range arr(m["event_reports"]) {
		row := obj(raw)
		refs := c.refs(row, "source_refs", fmt.Sprintf("$.event_reports[%d]", i), "transcript")
		if len(refs) == 0 {
			c.missing(fmt.Sprintf("$.event_reports[%d]", i))
			continue
		}
		subject := row["subject"]
		if subject == "target_customer" && !c.allBound(refs, "target_customer") {
			subject = "unknown"
			c.note(fmt.Sprintf("$.event_reports[%d].subject", i), "event_subject_unconfirmed")
		}
		card["event_reports"] = append(arr(card["event_reports"]), object{"event_type": row["event_type"], "state": row["state"], "subject": subject, "evidence_refs": list(refs)})
	}
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
	if nonBusiness {
		// A non-business recording must keep its explicit label instead of a
		// generic "无法判断/待复核" sentence that hides the model's correct
		// identification.
		summary = rc11NonBusinessConclusion
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
	if nonBusiness {
		verification = rc11NonBusinessVerify
	}
	card["conclusion"] = object{"current_state": summary, "business_impact": impact, "main_basis": list(basis), "verification": verification}
	next := "当前没有可执行动作，先核对证据与业务条件。"
	if nonBusiness {
		next = rc11NonBusinessNextStep
	} else if policy == "stop" {
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
