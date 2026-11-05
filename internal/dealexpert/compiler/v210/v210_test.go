package v210

import (
	"encoding/json"
	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler/v200"
	"os"
	"testing"
)

func v210Fixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	e, m := map[string]any{}, map[string]any{}
	for _, z := range []struct {
		n string
		v *map[string]any
	}{{"input", &e}, {"output", &m}} {
		b, err := os.ReadFile("../v200/testdata/example-" + z.n + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(b, z.v); err != nil {
			t.Fatal(err)
		}
	}
	id, err := v200.SnapshotID(must(t, e["input"]))
	if err != nil {
		t.Fatal(err)
	}
	e["input_snapshot_id"] = id
	m["input_snapshot_id"] = id
	m["protocol_version"] = "deal-analysis-v2.1"
	d := m["decision"].(map[string]any)
	d["state"] = "暂缓"
	d["confidence"] = map[string]any{"level": "medium", "scope": "scheme"}
	d["target"] = "方案甲"
	d["condition"] = map[string]any{"summary": "等家人商量", "source_refs": []any{"s2"}}
	m["additional_decisions"] = []any{}
	m["event_reports"] = []any{map[string]any{"event_type": "预约", "state": "planned", "subject": "target_customer", "source_refs": []any{"s2"}}}
	e["input"].(map[string]any)["protocol_version"] = "deal-input-v2.1"
	e["input"].(map[string]any)["context"].(map[string]any)["target_binding"] = "unresolved"
	ctx := e["input"].(map[string]any)["context"].(map[string]any)
	ctx["target_customer_id"] = nil
	ctx["binding_record_id"] = nil
	ctx["consultant_role_verified"] = false
	for _, raw := range e["input"].(map[string]any)["transcript_sources"].([]any) {
		r := raw.(map[string]any)
		r["subject_binding"] = "unresolved"
		r["speaker_id"] = nil
		r["binding_record_id"] = nil
	}
	for _, raw := range e["input"].(map[string]any)["business_facts"].([]any) {
		raw.(map[string]any)["target_customer_id"] = 1
	}
	m["coach"] = map[string]any{"observations": []any{map[string]any{"behavior": "professional_handoff", "source_refs": []any{"s3"}, "response_source_refs": []any{"s4"}}}, "improvement": nil}
	m["proposed_action"] = map[string]any{"type": "sales_follow_up", "template": "confirm_next_step", "source_refs": []any{"s2"}, "customer_window_source_refs": []any{"s4"}}
	d["condition"].(map[string]any)["kind"] = "family_discussion"
	d["outcome_signals"] = []any{}
	id, err = v200.SnapshotID(must(t, e["input"]))
	if err != nil {
		t.Fatal(err)
	}
	e["input_snapshot_id"] = id
	m["input_snapshot_id"] = id
	return must(t, e), must(t, m)
}
func must(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestV210SeparatesIntentAndPlannedEvent(t *testing.T) {
	in, m := v210Fixture(t)
	r, e := Compile(in, m, RulesVersion)
	if e != nil {
		t.Fatal(e)
	}
	var c map[string]any
	if e = json.Unmarshal(r.Card, &c); e != nil {
		t.Fatal(e)
	}
	if c["schema_version"] != "deal-card-v2.1" {
		t.Fatal(c["schema_version"])
	}
	if c["primary_action"] != nil {
		t.Fatal("unresolved target action")
	}
	if c["intent"].(map[string]any)["state"] != "暂缓" {
		t.Fatal(c["intent"])
	}
	if c["event_reports"].([]any)[0].(map[string]any)["state"] != "planned" {
		t.Fatal(c["event_reports"])
	}
}
func TestV210RejectsCompletedIntentEnum(t *testing.T) {
	in, m := v210Fixture(t)
	var x map[string]any
	json.Unmarshal(m, &x)
	x["decision"].(map[string]any)["state"] = "已预约"
	if ValidateIntermediate(must(t, x)) == nil {
		t.Fatal("accepted completed intent")
	}
	_ = in
}

func decoded(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	v, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return obj(v)
}
func sealed(t *testing.T, e, m map[string]any) ([]byte, []byte) {
	t.Helper()
	id, err := SnapshotID(must(t, e["input"]))
	if err != nil {
		t.Fatal(err)
	}
	e["input_snapshot_id"] = id
	m["input_snapshot_id"] = id
	return must(t, e), must(t, m)
}
func boundFixture(t *testing.T) ([]byte, []byte) {
	in, mid := v210Fixture(t)
	e, m := decoded(t, in), decoded(t, mid)
	input := obj(e["input"])
	ctx := obj(input["context"])
	ctx["target_binding"] = "single_customer"
	ctx["target_customer_id"] = json.Number("1")
	ctx["binding_record_id"] = "encounter-verified-1"
	ctx["identity_confirmed"] = true
	ctx["contact_authorized"] = true
	ctx["consultant_role_verified"] = true
	input["business_facts"] = []any{}
	obj(arr(input["transcript_sources"])[3])["text"] = "这样比较清楚，请在我和家人商量后联系我确认安排。"
	for _, raw := range arr(input["transcript_sources"]) {
		r := obj(raw)
		r["binding_record_id"] = "speaker-verified-1"
		r["speaker_id"] = "speaker-1"
		r["subject_binding"] = "consultant"
		if r["speaker"] == "客户" {
			r["subject_binding"] = "target_customer"
			r["speaker_id"] = "speaker-2"
		}
	}
	return sealed(t, e, m)
}
func compileCard(t *testing.T, in, mid []byte) (Result, map[string]any) {
	t.Helper()
	if err := ValidateSnapshot(in); err != nil {
		t.Fatal(err)
	}
	if err := ValidateIntermediate(mid); err != nil {
		t.Fatal(err)
	}
	r, err := Compile(in, mid, RulesVersion)
	if err != nil {
		t.Fatal(err)
	}
	return r, decoded(t, r.Card)
}
func TestV210BoundSalesPositive(t *testing.T) {
	in, mid := boundFixture(t)
	r, c := compileCard(t, in, mid)
	if r.Status != "analyzed" || c["primary_action"] == nil {
		t.Fatalf("no useful positive: %s %s", r.Status, r.Card)
	}
	a := obj(c["primary_action"])
	if a["due_provenance"] != "system_suggestion" || a["customer_window"] == nil {
		t.Fatal(a)
	}
	q := obj(a["customer_window"])["quote"]
	if q != "这样比较清楚，请在我和家人商量后联系我确认安排。" {
		t.Fatal(q)
	}
	if obj(c["decision"])["condition"] == nil {
		t.Fatal("condition removed")
	}
}
func TestV210UnknownTargetPreservesLocalCondition(t *testing.T) {
	in, mid := v210Fixture(t)
	_, c := compileCard(t, in, mid)
	if obj(c["decision"])["decision_state"] != "无法判断" || c["primary_action"] != nil {
		t.Fatal(c)
	}
	found := false
	for _, r := range arr(c["recording_observations"]) {
		if obj(r)["kind"] == "condition" {
			found = true
		}
	}
	if !found {
		t.Fatal("local condition lost")
	}
	if obj(c["intent"])["state"] != "暂缓" {
		t.Fatal("recording intent lost")
	}
}
func TestV210EventsNeverCompleteIntent(t *testing.T) {
	for _, state := range []string{"planned", "claimed_completed", "denied", "unknown"} {
		t.Run(state, func(t *testing.T) {
			in, mid := boundFixture(t)
			m := decoded(t, mid)
			obj(arr(m["event_reports"])[0])["state"] = state
			_, c := compileCard(t, in, must(t, m))
			if obj(c["decision"])["decision_state"] != "暂缓" || len(arr(c["business_facts"])) != 0 {
				t.Fatal(c)
			}
			if obj(arr(c["event_reports"])[0])["state"] != state {
				t.Fatal(c)
			}
		})
	}
}
func TestV210ClinicalCoachRejected(t *testing.T) {
	in, mid := boundFixture(t)
	m := decoded(t, mid)
	row := obj(arr(obj(m["coach"])["observations"])[0])
	row["behavior"] = "支具或理疗"
	if ValidateIntermediate(must(t, m)) == nil {
		t.Fatal("clinical coach accepted")
	}
	_ = in
}
func TestV210ModelCannotWriteTimingOrQuote(t *testing.T) {
	_, mid := boundFixture(t)
	for _, field := range []string{"due_basis", "due_quote", "quote", "action", "goal", "suggested_content"} {
		t.Run(field, func(t *testing.T) {
			m := decoded(t, mid)
			obj(m["proposed_action"])[field] = "周末48小时支具方案"
			if ValidateIntermediate(must(t, m)) == nil {
				t.Fatal("unsafe generated field accepted")
			}
		})
	}
}
func TestV210CoachResponseNeedsIndependentBoundSpeaker(t *testing.T) {
	in, mid := boundFixture(t)
	_, c := compileCard(t, in, mid)
	if obj(arr(obj(c["coach"])["observations"])[0])["customer_response"] == nil {
		t.Fatal("legitimate response lost")
	}
	m := decoded(t, mid)
	obj(arr(obj(m["coach"])["observations"])[0])["response_source_refs"] = []any{"s3"}
	_, c = compileCard(t, in, must(t, m))
	if obj(arr(obj(c["coach"])["observations"])[0])["customer_response"] != nil {
		t.Fatal("staff statement became response")
	}
}
func TestV210UnboundInternalHandoffPositive(t *testing.T) {
	in, mid := v210Fixture(t)
	m := decoded(t, mid)
	obj(m["proposed_action"])["type"] = "internal_coordination"
	obj(m["proposed_action"])["template"] = "professional_handoff"
	_, c := compileCard(t, in, must(t, m))
	if c["primary_action"] == nil || obj(c["primary_action"])["type"] != "internal_coordination" {
		t.Fatal("all-review incorrectly erased safe handoff")
	}
}
func TestV210ConditionsSurviveNonDeferredState(t *testing.T) {
	in, mid := boundFixture(t)
	m := decoded(t, mid)
	obj(m["decision"])["state"] = "有意向但未决定"
	_, c := compileCard(t, in, must(t, m))
	if obj(c["decision"])["condition"] == nil {
		t.Fatal("condition erased by coarse state")
	}
}
func TestV210DeterministicAndRejectsDuplicateJSON(t *testing.T) {
	in, mid := boundFixture(t)
	a, _ := compileCard(t, in, mid)
	b, _ := compileCard(t, in, mid)
	if string(a.Card) != string(b.Card) {
		t.Fatal("non-deterministic")
	}
	if ValidateIntermediate([]byte(`{"protocol_version":"a","protocol_version":"b"}`)) == nil {
		t.Fatal("duplicate key accepted")
	}
}

func TestV210RejectsFalseBindingBeforeModelCall(t *testing.T) {
	in, mid := boundFixture(t)
	for _, name := range []string{"missing_identity", "missing_record", "mixed_speaker", "target_role"} {
		t.Run(name, func(t *testing.T) {
			e, m := decoded(t, in), decoded(t, mid)
			input := obj(e["input"])
			ctx := obj(input["context"])
			r := obj(arr(input["transcript_sources"])[1])
			switch name {
			case "missing_identity":
				ctx["identity_confirmed"] = false
			case "missing_record":
				r["binding_record_id"] = nil
			case "mixed_speaker":
				r["subject_binding"] = "mixed"
			case "target_role":
				r["speaker"] = "咨询师"
			}
			a, b := sealed(t, e, m)
			if ValidateSnapshot(a) == nil {
				t.Fatal("invalid ownership accepted")
			}
			if _, err := Compile(a, b, RulesVersion); err == nil {
				t.Fatal("invalid ownership compiled")
			}
		})
	}
}
func TestV210MissingEventReferenceDegradesLocally(t *testing.T) {
	in, mid := boundFixture(t)
	m := decoded(t, mid)
	obj(arr(m["event_reports"])[0])["source_refs"] = []any{"s999"}
	_, c := compileCard(t, in, must(t, m))
	if len(arr(c["event_reports"])) != 0 {
		t.Fatal("invented event evidence retained")
	}
	if obj(c["decision"])["decision_state"] != "暂缓" {
		t.Fatal("local event error erased supported decision")
	}
}
func TestV210ClinicalTextCannotEscapeActionTemplate(t *testing.T) {
	in, mid := boundFixture(t)
	m := decoded(t, mid)
	obj(m["clinical_risk"])["assessment"] = "unresolved"
	obj(m["clinical_risk"])["summary"] = "员工宣称已确诊且需理疗"
	obj(m["clinical_risk"])["source_refs"] = []any{"s3"}
	obj(m["proposed_action"])["type"] = "internal_coordination"
	obj(m["proposed_action"])["template"] = "professional_handoff"
	_, c := compileCard(t, in, must(t, m))
	if obj(c["clinical_conflict"])["reason"] == "员工宣称已确诊且需理疗" {
		t.Fatal("diagnosis promoted")
	}
	if c["primary_action"] == nil || obj(c["primary_action"])["suggested_content"] != nil {
		t.Fatal("unsafe handoff")
	}
}

func TestV210EventsCannotClaimUnknownPersonalSubject(t *testing.T) {
	in, mid := v210Fixture(t)
	_, c := compileCard(t, in, mid)
	if obj(arr(c["event_reports"])[0])["subject"] != "unknown" {
		t.Fatal("model invented target event binding")
	}
}
func TestV210VerifiedPaymentRemainsSeparateAndBlocksSales(t *testing.T) {
	in, mid := boundFixture(t)
	e, m := decoded(t, in), decoded(t, mid)
	input := obj(e["input"])
	input["business_facts"] = []any{object{"id": "f1", "type": "付款", "text": "系统已确认付款", "source_record_id": json.Number("701"), "occurred_at": nil, "target_customer_id": json.Number("1")}}
	in, mid = sealed(t, e, m)
	_, c := compileCard(t, in, mid)
	if len(arr(c["business_facts"])) != 1 || c["primary_action"] != nil {
		t.Fatal("payment lost or sales created")
	}
	if obj(c["decision"])["decision_state"] != "暂缓" {
		t.Fatal("payment changed current intent")
	}
}
func TestV210HashTamperAndMissingSourcesFailSafely(t *testing.T) {
	in, mid := boundFixture(t)
	e := decoded(t, in)
	obj(e["input"])["protocol_version"] = "deal-input-v2"
	if _, err := Compile(must(t, e), mid, RulesVersion); err == nil {
		t.Fatal("invalid input accepted")
	}
	m := decoded(t, mid)
	obj(m["decision"])["source_refs"] = []any{"s999"}
	_, c := compileCard(t, in, must(t, m))
	if obj(c["intent"])["state"] != "无法判断" {
		t.Fatal("unsupported intent retained")
	}
}

func TestV210SeparateSchemeDecisions(t *testing.T) {
	in, mid := boundFixture(t)
	e, m := decoded(t, in), decoded(t, mid)
	input := obj(e["input"])
	source := decoded(t, must(t, arr(input["transcript_sources"])[3]))
	source["id"] = "s5"
	source["text"] = "手术方案我明确不考虑。我只考虑方案甲，先和家人商量。"
	input["transcript_sources"] = append(arr(input["transcript_sources"]), source)
	d := obj(m["decision"])
	other := decoded(t, must(t, d))
	other["target"] = "手术方案"
	other["state"] = "明确不考虑"
	other["condition"] = nil
	other["source_refs"] = []any{"s5"}
	other["confidence"] = map[string]any{"level": "low", "scope": "scheme"}
	m["additional_decisions"] = []any{other}
	in, mid = sealed(t, e, m)
	_, c := compileCard(t, in, mid)
	schemes := arr(c["scheme_decisions"])
	if len(schemes) != 2 {
		t.Fatal("different schemes collapsed")
	}
	if obj(schemes[0])["target"] == obj(schemes[1])["target"] || obj(schemes[1])["decision_state"] != "明确不考虑" {
		t.Fatal(schemes)
	}
}

func TestV210OneSchemeEvidenceFailureDoesNotEraseAnother(t *testing.T) {
	in, mid := boundFixture(t)
	m := decoded(t, mid)
	d := obj(m["decision"])
	other := decoded(t, must(t, d))
	other["target"] = "手术方案"
	other["state"] = "明确不考虑"
	other["source_refs"] = []any{"s999"}
	other["condition"] = nil
	m["additional_decisions"] = []any{other}
	_, c := compileCard(t, in, must(t, m))
	schemes := arr(c["scheme_decisions"])
	if len(schemes) != 2 {
		t.Fatalf("want two schemes, got %d", len(schemes))
	}
	if obj(schemes[0])["decision_state"] != "暂缓" {
		t.Fatalf("primary scheme erased: %#v", schemes[0])
	}
	if obj(schemes[1])["decision_state"] != "无法判断" {
		t.Fatalf("invalid secondary scheme retained: %#v", schemes[1])
	}
}

func TestV210FinalCardRejectsSchemeTampering(t *testing.T) {
	in, mid := boundFixture(t)
	_, card := compileCard(t, in, mid)
	for _, kind := range []string{"completed_intent", "different_primary", "unknown_without_review", "missing_condition_evidence"} {
		t.Run(kind, func(t *testing.T) {
			c := decoded(t, must(t, card))
			s := obj(arr(c["scheme_decisions"])[0])
			switch kind {
			case "completed_intent":
				s["decision_state"] = "已付款"
			case "different_primary":
				s["target"] = "另一个方案"
			case "unknown_without_review":
				s["decision_state"] = "无法判断"
				s["state_basis"] = "unknown"
				c["status"] = "analyzed"
			case "missing_condition_evidence":
				s["condition_evidence_refs"] = []any{}
			}
			if ValidateCard(must(t, c)) == nil {
				t.Fatal("tampered scheme accepted")
			}
		})
	}
}

func TestV210ConfidenceIsLocalAndCannotCertifyIdentity(t *testing.T) {
	in, mid := boundFixture(t)
	m := decoded(t, mid)
	other := decoded(t, must(t, m["decision"]))
	other["target"] = "手术方案"
	other["state"] = "明确不考虑"
	other["condition"] = nil
	other["confidence"] = object{"level": "low", "scope": "scheme"}
	m["additional_decisions"] = []any{other}
	_, c := compileCard(t, in, must(t, m))
	schemes := arr(c["scheme_decisions"])
	if obj(obj(schemes[0])["confidence"])["level"] == "low" {
		t.Fatal("secondary low confidence erased primary confidence")
	}
	if obj(obj(schemes[1])["confidence"])["level"] != "low" {
		t.Fatal("local uncertainty lost")
	}
	in, mid = v210Fixture(t)
	_, c = compileCard(t, in, mid)
	if obj(obj(c["decision"])["confidence"])["level"] != "unknown" {
		t.Fatal("model confidence certified unknown personal identity")
	}
}

func TestV210UnboundSchemesKeepSeparateRecordingObservations(t *testing.T) {
	in, mid := v210Fixture(t)
	m := decoded(t, mid)
	other := decoded(t, must(t, m["decision"]))
	other["target"] = "手术方案"
	other["state"] = "明确不考虑"
	other["condition"] = nil
	m["additional_decisions"] = []any{other}
	_, c := compileCard(t, in, must(t, m))
	seen := map[string]bool{}
	for _, raw := range arr(c["recording_observations"]) {
		r := obj(raw)
		if r["kind"] == "intent" {
			seen[str(r["target"])] = true
		}
	}
	if !seen["方案甲"] || !seen["手术方案"] {
		t.Fatal("unbound recording schemes lost or merged")
	}
	for _, raw := range arr(c["scheme_decisions"]) {
		if obj(raw)["decision_state"] != "无法判断" {
			t.Fatal("unbound scheme promoted to personal state")
		}
	}
}

func TestV210ClinicalObservationNeverPublishesModelDiagnosis(t *testing.T) {
	in, mid := v210Fixture(t)
	m := decoded(t, mid)
	obj(m["clinical_risk"])["summary"] = "推断已确诊神经损伤并将加重，应立即治疗"
	obj(m["clinical_risk"])["source_refs"] = []any{"s3"}
	_, c := compileCard(t, in, must(t, m))
	for _, raw := range arr(c["recording_observations"]) {
		r := obj(raw)
		if r["kind"] == "clinical_statement" && r["summary"] == obj(m["clinical_risk"])["summary"] {
			t.Fatal("model diagnosis published as recording observation")
		}
	}
}
