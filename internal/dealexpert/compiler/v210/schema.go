package v210

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"
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

// card_rc8.schema.json is card.schema.json with only the rules_version const
// changed. It is a separate embedded resource so rc7 schema bytes and hashes
// remain untouched.
//
//go:embed card_rc8.schema.json
var cardRC8Schema []byte

// card_rc9.schema.json is card_rc8.schema.json with only the rules_version
// const changed. rc9 changes prompt guidance only, so it reuses rc8's compiler
// logic and needs no other schema difference.
//
//go:embed card_rc9.schema.json
var cardRC9Schema []byte

// card_rc10.schema.json is card_rc9.schema.json with only the rules_version
// const changed. rc10 changes prompt guidance only, so it reuses rc9's compiler
// logic and needs no other schema difference.
//
//go:embed card_rc10.schema.json
var cardRC10Schema []byte

// rc11 introduces an additive analysis_scope field in the intermediate and
// final schemas and two extra recording_observation kinds. The rc11 intermediate
// schema is a separate embedded resource so rc7-rc10 keep their exact bytes;
// analysis_scope is optional there so an rc10 saved response can still be
// recompiled under rc11 for counterfactual checks.
//
//go:embed intermediate_rc11.schema.json
var intermediateRC11Schema []byte

//go:embed card_rc11.schema.json
var cardRC11Schema []byte

// rc12 adds the additive local_observations intermediate array and extends the
// recording observation kinds with a structured timing slot. Its intermediate
// schema is separate so rc7-rc11 keep their exact bytes; local_observations is
// optional there so an rc11 saved response can still be recompiled under rc12.
//
//go:embed intermediate_rc12.schema.json
var intermediateRC12Schema []byte

//go:embed card_rc12.schema.json
var cardRC12Schema []byte

// rc12.1 repairs the rc12 card enum (kind=other) without changing the rc12
// intermediate contract, so the same intermediate bytes are reused deliberately.
//
//go:embed card_rc12p1.schema.json
var cardRC12P1Schema []byte

var schemaOnce sync.Once
var schemaSets map[string][3]*jsonschema.Schema
var schemaError error

// Contracts returns independent copies of the exact schemas used by Compile,
// for immutable invocation bundles. Mutating them cannot change the compiler.
func Contracts() (input, intermediate, final []byte) {
	return ContractsForVersion(RulesVersion)
}

// ContractsForVersion returns the frozen schemas for one explicitly selected
// v2.1 rules version. rc11 also owns an intermediate schema because analysis_scope
// is additive; older versions keep the shared intermediate bytes.
func ContractsForVersion(version string) (input, intermediate, final []byte) {
	var card, mid []byte
	switch version {
	case RulesVersion:
		card, mid = cardSchema, intermediateSchema
	case RulesVersionRC8:
		card, mid = cardRC8Schema, intermediateSchema
	case RulesVersionRC9:
		card, mid = cardRC9Schema, intermediateSchema
	case RulesVersionRC10:
		card, mid = cardRC10Schema, intermediateSchema
	case RulesVersionRC11:
		card, mid = cardRC11Schema, intermediateRC11Schema
	case RulesVersionRC12:
		card, mid = cardRC12Schema, intermediateRC12Schema
	case RulesVersionRC12P1:
		card, mid = cardRC12P1Schema, intermediateRC12Schema
	default:
		return nil, nil, nil
	}
	return append([]byte(nil), inputSchema...), append([]byte(nil), mid...), append([]byte(nil), card...)
}

func schemaSet(version string) ([3]*jsonschema.Schema, error) {
	if err := loadSchemas(); err != nil {
		return [3]*jsonschema.Schema{}, err
	}
	set, ok := schemaSets[version]
	if !ok {
		return [3]*jsonschema.Schema{}, fmt.Errorf("unsupported rules version %q", version)
	}
	return set, nil
}

// ValidateSnapshot verifies input structure, canonical binding and source
// identity before the orchestration layer spends a model call.
func ValidateSnapshot(raw []byte) error {
	set, err := schemaSet(RulesVersion)
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	if err = set[0].Validate(v); err != nil {
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
	if err := c.index(); err != nil {
		return err
	}
	return c.validateOwnership()
}

func loadSchemas() error {
	schemaOnce.Do(func() {
		schemaSets = map[string][3]*jsonschema.Schema{}
		for _, variant := range []struct {
			version string
			mid     []byte
			card    []byte
		}{
			{RulesVersion, intermediateSchema, cardSchema},
			{RulesVersionRC8, intermediateSchema, cardRC8Schema},
			{RulesVersionRC9, intermediateSchema, cardRC9Schema},
			{RulesVersionRC10, intermediateSchema, cardRC10Schema},
			{RulesVersionRC11, intermediateRC11Schema, cardRC11Schema},
			{RulesVersionRC12, intermediateRC12Schema, cardRC12Schema},
			{RulesVersionRC12P1, intermediateRC12Schema, cardRC12P1Schema},
		} {
			var set [3]*jsonschema.Schema
			for i, raw := range [][]byte{inputSchema, variant.mid, variant.card} {
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
				set[i], err = c.Compile(id)
				if err != nil {
					schemaError = err
					return
				}
			}
			schemaSets[variant.version] = set
		}
	})
	return schemaError
}

// ValidateCard verifies structure and server-owned reference relationships.
func ValidateCard(raw []byte) error {
	return ValidateCardForVersion(raw, RulesVersion)
}

// ValidateCardForVersion validates a card against its selected rules version so
// an rc8 card can never be accepted as rc7 (or the reverse).
func ValidateCardForVersion(raw []byte, version string) error {
	set, err := schemaSet(version)
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	if err = set[2].Validate(v); err != nil {
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
	schemes := arr(card["scheme_decisions"])
	if len(schemes) == 0 || !reflect.DeepEqual(card["decision"], schemes[0]) {
		return fmt.Errorf("primary scheme differs from primary decision")
	}
	for _, raw := range schemes {
		scheme := obj(raw)
		state := str(scheme["decision_state"])
		if state == "无法判断" {
			if scheme["state_basis"] != "unknown" || card["status"] != "needs_review" || len(arr(scheme["evidence_refs"])) != 0 || obj(scheme["confidence"])["level"] != "unknown" {
				return fmt.Errorf("unknown scheme requires review without decision evidence")
			}
		} else if scheme["target"] == nil || scheme["state_basis"] != "transcript_judgment" || len(arr(scheme["evidence_refs"])) == 0 {
			return fmt.Errorf("scheme lacks target or decision evidence")
		}
		for _, id := range stringsAt(scheme["evidence_refs"]) {
			if evs[id]["source_kind"] != "transcript" {
				return fmt.Errorf("scheme intent uses business fact")
			}
		}
		conditionRefs := stringsAt(scheme["condition_evidence_refs"])
		if scheme["condition"] != nil {
			if scheme["condition_kind"] == nil || scheme["condition_due_quote"] == nil || len(conditionRefs) == 0 {
				return fmt.Errorf("scheme condition has no bound evidence")
			}
		} else if state == "暂缓" || scheme["condition_kind"] != nil || scheme["condition_due_quote"] != nil || len(conditionRefs) != 0 {
			return fmt.Errorf("inconsistent scheme condition")
		}
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
		if obj(a)["type"] == "sales_follow_up" && (card["status"] == "needs_review" || card["confidence"] == "low" || policy == "stop" || policy == "review") {
			return fmt.Errorf("blocked action retained")
		}
		action := obj(a)
		if policy == "pause_clinical" && action["type"] != "internal_coordination" && action["type"] != "internal_review" {
			return fmt.Errorf("clinical hold action is not internal coordination")
		}
		timed := len(arr(action["timing_evidence_refs"])) > 0
		if (action["due_provenance"] == "customer_source") != timed || (action["due_quote"] != nil) != timed {
			return fmt.Errorf("inconsistent timing provenance")
		}
	}
	return nil
}

func ValidateIntermediate(raw []byte) error {
	set, err := schemaSet(RulesVersion)
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	return set[1].Validate(v)
}
