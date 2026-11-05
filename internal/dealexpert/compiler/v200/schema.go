package v200

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed input.schema.json
var inputSchema []byte

//go:embed intermediate.schema.json
var intermediateSchema []byte

//go:embed card.schema.json
var cardSchema []byte

var schemaOnce sync.Once
var schemas [3]*jsonschema.Schema
var schemaError error

// Contracts returns independent copies of the exact schemas used by Compile,
// for immutable invocation bundles. Mutating them cannot change the compiler.
func Contracts() (input, intermediate, final []byte) {
	return append([]byte(nil), inputSchema...), append([]byte(nil), intermediateSchema...), append([]byte(nil), cardSchema...)
}

// ValidateSnapshot verifies input structure, canonical binding and source
// identity before the orchestration layer spends a model call.
func ValidateSnapshot(raw []byte) error {
	if err := loadSchemas(); err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	if err = schemas[0].Validate(v); err != nil {
		return err
	}
	e := obj(v)
	in := obj(e["input"])
	hash, err := canonicalSnapshotID(in)
	if err != nil {
		return err
	}
	if e["input_snapshot_id"] != hash {
		return fmt.Errorf("snapshot mismatch")
	}
	c := compiler{input: in, context: obj(in["context"]), sources: map[string]source{}, diagnostics: []Diagnostic{}}
	return c.index()
}

func loadSchemas() error {
	schemaOnce.Do(func() {
		for i, raw := range [][]byte{inputSchema, intermediateSchema, cardSchema} {
			var doc object
			if err := json.Unmarshal(raw, &doc); err != nil {
				schemaError = err
				return
			}
			c := jsonschema.NewCompiler()
			c.AssertFormat()
			id := str(doc["$id"])
			if err := c.AddResource(id, doc); err != nil {
				schemaError = err
				return
			}
			var err error
			schemas[i], err = c.Compile(id)
			if err != nil {
				schemaError = err
				return
			}
		}
	})
	return schemaError
}

// ValidateCard verifies structure and server-owned reference relationships.
func ValidateCard(raw []byte) error {
	if err := loadSchemas(); err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	if err = schemas[2].Validate(v); err != nil {
		return err
	}
	card := obj(v)
	if err := validateReferences(card); err != nil {
		return err
	}
	return validateBusinessRules(card)
}

func validateReferences(card object) error {
	evidence := map[string]object{}
	sourceIDs := map[string]bool{}
	for _, row := range arr(card["evidence_refs"]) {
		e := obj(row)
		id := str(e["id"])
		if _, ok := evidence[id]; ok {
			return fmt.Errorf("duplicate evidence %s", id)
		}
		evidence[id] = e
		ref := str(e["source_ref"])
		if sourceIDs[ref] {
			return fmt.Errorf("duplicate evidence source %s", ref)
		}
		sourceIDs[ref] = true
	}
	seen := map[string]map[string]bool{}
	for _, use := range evidenceUses(card) {
		for _, id := range use.refs {
			if _, ok := evidence[id]; !ok {
				return fmt.Errorf("dangling evidence %s at %s", id, use.path)
			}
			if use.usage != "结论" && use.usage != "复盘" && evidence[id]["source_kind"] != "transcript" {
				return fmt.Errorf("wrong evidence kind at %s", use.path)
			}
			if seen[id] == nil {
				seen[id] = map[string]bool{}
			}
			seen[id][use.usage] = true
		}
	}
	for id, e := range evidence {
		if len(seen[id]) == 0 {
			return fmt.Errorf("unused evidence %s", id)
		}
		uses := stringsAt(e["used_for"])
		if len(uses) != len(seen[id]) {
			return fmt.Errorf("inconsistent evidence usage %s", id)
		}
		for _, u := range uses {
			if !seen[id][u] {
				return fmt.Errorf("invalid usage %s", id)
			}
		}
		isTranscript := str(e["source_kind"]) == "transcript"
		if isTranscript != strings.HasPrefix(str(e["source_ref"]), "s") {
			return fmt.Errorf("inconsistent source kind")
		}
		if isTranscript && str(e["speaker"]) == "系统事实" {
			return fmt.Errorf("transcript promoted to fact")
		}
		if !isTranscript && (str(e["speaker"]) != "系统事实" || e["start_ms"] != nil) {
			return fmt.Errorf("fact has recording metadata")
		}
		if isTranscript {
			if (e["start_ms"] == nil) != (e["timing_basis"] == "unknown") {
				return fmt.Errorf("inconsistent time metadata")
			}
			if (e["speaker"] == "未知") != (e["role_basis"] == "unknown") || e["role_basis"] == "system_verified" {
				return fmt.Errorf("inconsistent role metadata")
			}
		} else if e["timing_basis"] != "unknown" || e["role_basis"] != "system_verified" {
			return fmt.Errorf("inconsistent fact metadata")
		}
	}
	return nil
}

func validateBusinessRules(card object) error {
	evs := map[string]object{}
	for _, raw := range arr(card["evidence_refs"]) {
		e := obj(raw)
		evs[str(e["id"])] = e
	}
	d := obj(card["decision"])
	state := str(d["decision_state"])
	basis := str(d["state_basis"])
	if state == "无法判断" && (basis != "unknown" || card["status"] != "needs_review") {
		return fmt.Errorf("unknown decision must need review")
	}
	if state != "无法判断" && len(arr(d["evidence_refs"])) == 0 {
		return fmt.Errorf("decision has no evidence")
	}
	if state == "暂缓" && len(arr(d["condition_evidence_refs"])) == 0 {
		return fmt.Errorf("condition has no evidence")
	}
	factType := map[string]string{}
	hasRefund := false
	for _, raw := range arr(card["business_facts"]) {
		f := obj(raw)
		for _, id := range stringsAt(f["evidence_refs"]) {
			if evs[id]["source_kind"] != "business_fact" {
				return fmt.Errorf("business event uses transcript")
			}
			factType[id] = str(f["type"])
		}
		hasRefund = hasRefund || f["type"] == "退款"
	}
	if basis == "verified_fact" {
		expected := map[string]string{"已预约": "预约", "已交定金": "定金", "已付款": "付款"}[state]
		if expected == "" {
			return fmt.Errorf("judgment promoted to verified fact")
		}
		for _, id := range stringsAt(d["evidence_refs"]) {
			if factType[id] != expected {
				return fmt.Errorf("wrong event type for verified decision")
			}
		}
	}
	if basis == "reported_only" || basis == "transcript_judgment" {
		for _, id := range stringsAt(d["evidence_refs"]) {
			if evs[id]["source_kind"] != "transcript" {
				return fmt.Errorf("transcript judgment uses fact")
			}
		}
	}
	if hasRefund && (card["status"] != "needs_review" || obj(card["conclusion"])["business_impact"] != "无法判断") {
		return fmt.Errorf("refund requires review of net outcome")
	}
	if obj(card["conclusion"])["business_impact"] == "已成交可复盘" {
		paid := false
		for _, typ := range factType {
			paid = paid || typ == "定金" || typ == "付款"
		}
		if !paid || hasRefund {
			return fmt.Errorf("deal impact requires verified payment without unresolved refund")
		}
	}
	policy := str(card["contact_policy"])
	a := card["primary_action"]
	if a != nil {
		if card["status"] == "needs_review" || card["confidence"] == "low" || policy == "stop" || policy == "review" {
			return fmt.Errorf("blocked action retained")
		}
		action := obj(a)
		if policy == "pause_clinical" && action["type"] != "internal_coordination" {
			return fmt.Errorf("clinical hold action is not internal coordination")
		}
		timed := len(arr(action["timing_evidence_refs"])) > 0
		if (action["due_provenance"] == "customer_source") != timed || (action["due_quote"] != nil) != timed {
			return fmt.Errorf("inconsistent timing provenance")
		}
	}
	return nil
}
