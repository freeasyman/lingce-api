package dealexpert

import (
	"encoding/json"
	"strings"
	"testing"
)

func dimension() map[string]any {
	return map[string]any{
		"assessment":          "scored",
		"score":               3,
		"comment":             "按要求完成",
		"evidence_refs":       []any{"ev_1"},
		"impact_on_deal":      nil,
		"highlight_narrative": nil,
	}
}

func baseCardV11() map[string]any {
	dimensions := map[string]any{
		"开场建立权威": dimension(), "需求探索": dimension(), "问题放大": dimension(),
		"专业呈现": dimension(), "方案定制": dimension(), "异议化解": dimension(),
		"促成与收尾": dimension(),
	}
	return map[string]any{
		"schema_version": dealCardVersion,
		"conclusion": map[string]any{
			"current_state":   "有明确意向但未锁定下一步",
			"business_impact": "仍可推进",
			"main_basis":      []any{"ev_1"},
			"verification":    "记录是否预约",
		},
		"decision": map[string]any{
			"decision_state":      "有意向但未决定",
			"state_basis":         "transcript_judgment",
			"evidence_refs":       []any{"ev_1"},
			"condition":           nil,
			"condition_due_quote": nil,
			"trust": []any{map[string]any{
				"trust_target": "医生", "state": "部分建立", "summary": "认可方案", "evidence_refs": []any{"ev_1"},
			}},
			"need_state":         "明确",
			"need_summary":       "想解决近视",
			"need_evidence_refs": []any{"ev_1"},
			"concerns": []any{map[string]any{
				"type": "家属或共同决策人", "summary": "要和家里人商量", "is_primary": true, "evidence_refs": []any{"ev_1"},
			}},
			"outcome_signals": []any{},
		},
		"clinical_conflict": map[string]any{"active": false, "reason": nil, "evidence_refs": []any{}},
		"contact_policy":    "allow",
		"product_findings": []any{map[string]any{
			"product_id": 3, "product_name": "全飞秒Pro近视手术", "finding_type": "已正确推荐",
			"customer_evidence": "我想做全飞秒", "matching_reason": "客户明确表达需求",
			"recommended_by_consultant": true, "requires_clinical_confirmation": false,
			"suggested_action": "补发方案资料", "product_version": "1", "confidence": "high",
			"evidence_refs": []any{"ev_1"},
		}},
		"primary_action": map[string]any{
			"type": "sales_follow_up", "goal": "确认家属顾虑", "action": "今天打电话确认",
			"suggested_content": "想确认一下家人最担心什么", "owner_employee_id": nil,
			"due_basis": "today", "due_quote": nil, "source_evidence_refs": []any{"ev_1"},
		},
		"secondary_findings": []any{},
		"evidence_refs": []any{map[string]any{
			"id": "ev_1", "quote": "我想做全飞秒", "speaker": "客户", "source_ref": "s1",
			"start_ms": 12000, "used_for": []any{"结论", "需求"},
		}},
		"quality": map[string]any{"standard_version": dealQualityStandard, "dimensions": dimensions},
		"coach": map[string]any{
			"successful_practices": []any{},
			"improvement":          nil,
			"refund_review":        nil,
		},
		"confidence": "high",
		"unknowns":   []any{},
	}
}

func validationCtxV11() cardValidationContext {
	start := 12000
	return cardValidationContext{
		ConsultantName: "盖珊珊",
		Sources: map[string]evidenceSource{
			"s1": {ID: "s1", Text: "我想做全飞秒，但是要回去和家里人商量。", Speaker: "客户", StartMS: &start},
		},
		Facts:         map[string]businessFactRef{"f1": {Type: "付款"}},
		Catalog:       map[int64]catalogRef{3: {Name: "全飞秒Pro近视手术", Version: "1"}},
		AllowMissed:   true,
		AllowExtended: false,
	}
}

func validateCard(t *testing.T, card map[string]any, ctx cardValidationContext) (map[string]any, error) {
	t.Helper()
	schema, _, err := loadDealCardSchema("")
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal card: %v", err)
	}
	return validateDealCard(string(raw), schema, ctx)
}

func TestValidateDealCardAcceptsValidV11Card(t *testing.T) {
	if _, err := validateCard(t, baseCardV11(), validationCtxV11()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDealCardAllowsNullAction(t *testing.T) {
	card := baseCardV11()
	card["primary_action"] = nil
	if _, err := validateCard(t, card, validationCtxV11()); err != nil {
		t.Fatalf("null action should be allowed: %v", err)
	}
}

func TestValidateDealCardRejectsUnknownEvidenceRef(t *testing.T) {
	card := baseCardV11()
	card["conclusion"].(map[string]any)["main_basis"] = []any{"ev_9"}
	if _, err := validateCard(t, card, validationCtxV11()); err == nil || !strings.Contains(err.Error(), "unknown evidence ref") {
		t.Fatalf("expected unknown evidence error, got %v", err)
	}
}

func TestValidateDealCardRejectsQuoteNotInSource(t *testing.T) {
	card := baseCardV11()
	card["evidence_refs"] = []any{map[string]any{
		"id": "ev_1", "quote": "客户说一定要做", "speaker": "客户", "source_ref": "s1",
		"start_ms": 12000, "used_for": []any{"结论"},
	}}
	if _, err := validateCard(t, card, validationCtxV11()); err == nil || !strings.Contains(err.Error(), "quote not found") {
		t.Fatalf("expected quote error, got %v", err)
	}
}

func TestValidateDealCardRepairsSpeakerMismatch(t *testing.T) {
	card := baseCardV11()
	evidence := card["evidence_refs"].([]any)[0].(map[string]any)
	evidence["speaker"] = "咨询师"
	validated, err := validateCard(t, card, validationCtxV11())
	if err != nil {
		t.Fatalf("speaker mismatch should be repaired: %v", err)
	}
	repaired := validated["evidence_refs"].([]any)[0].(map[string]any)
	if repaired["speaker"] != "客户" {
		t.Fatalf("speaker should be repaired to 客户, got %v", repaired["speaker"])
	}
}

func TestValidateDealCardRepairsStartMismatch(t *testing.T) {
	card := baseCardV11()
	evidence := card["evidence_refs"].([]any)[0].(map[string]any)
	evidence["start_ms"] = 5
	validated, err := validateCard(t, card, validationCtxV11())
	if err != nil {
		t.Fatalf("start_ms mismatch should be repaired: %v", err)
	}
	repaired := validated["evidence_refs"].([]any)[0].(map[string]any)
	if repaired["start_ms"] != float64(12000) {
		t.Fatalf("start_ms should be repaired to 12000, got %v", repaired["start_ms"])
	}
}

func TestValidateDealCardRejectsVerifiedFactWithoutFact(t *testing.T) {
	card := baseCardV11()
	decision := card["decision"].(map[string]any)
	decision["decision_state"] = "已付款"
	decision["state_basis"] = "verified_fact"
	ctx := validationCtxV11()
	ctx.Facts = map[string]businessFactRef{}
	if _, err := validateCard(t, card, ctx); err == nil || !strings.Contains(err.Error(), "verified_fact") {
		t.Fatalf("expected verified_fact error, got %v", err)
	}
}

func TestValidateDealCardDropsActionForStopPolicy(t *testing.T) {
	card := baseCardV11()
	card["contact_policy"] = "stop"
	validated, err := validateCard(t, card, validationCtxV11())
	if err != nil {
		t.Fatalf("stop policy should keep the card and drop the action: %v", err)
	}
	if validated["primary_action"] != nil {
		t.Fatal("primary_action should be dropped for stop policy")
	}
}

func TestValidateDealCardRejectsClinicalConflictWithFindings(t *testing.T) {
	card := baseCardV11()
	card["clinical_conflict"] = map[string]any{"active": true, "reason": "术后问题未解决", "evidence_refs": []any{"ev_1"}}
	if _, err := validateCard(t, card, validationCtxV11()); err == nil {
		t.Fatal("clinical conflict with product findings should be rejected")
	}
}

func TestValidateDealCardDropsMissedWhenDisabled(t *testing.T) {
	card := baseCardV11()
	card["product_findings"].([]any)[0].(map[string]any)["finding_type"] = "可能漏推"
	ctx := validationCtxV11()
	ctx.AllowMissed = false
	validated, err := validateCard(t, card, ctx)
	if err != nil {
		t.Fatalf("disallowed finding should be dropped, not rejected: %v", err)
	}
	if len(validated["product_findings"].([]any)) != 0 {
		t.Fatal("missed finding should be dropped when disabled")
	}
}

func TestValidateDealCardDedupesFindings(t *testing.T) {
	card := baseCardV11()
	findings := card["product_findings"].([]any)
	card["product_findings"] = append(findings, findings[0])
	validated, err := validateCard(t, card, validationCtxV11())
	if err != nil {
		t.Fatalf("duplicate finding should be deduped: %v", err)
	}
	if len(validated["product_findings"].([]any)) != 1 {
		t.Fatal("duplicate finding should be removed")
	}
}

func TestValidateDealCardDropsActionForLowConfidence(t *testing.T) {
	card := baseCardV11()
	card["confidence"] = "low"
	validated, err := validateCard(t, card, validationCtxV11())
	if err != nil {
		t.Fatalf("low confidence should keep the card and drop the action: %v", err)
	}
	if validated["primary_action"] != nil {
		t.Fatal("primary_action should be dropped for low confidence")
	}
}

func TestValidateDealCardRejectsDuplicateEvidenceID(t *testing.T) {
	card := baseCardV11()
	evidence := card["evidence_refs"].([]any)
	card["evidence_refs"] = append(evidence, evidence[0])
	if _, err := validateCard(t, card, validationCtxV11()); err == nil || !strings.Contains(err.Error(), "duplicate evidence") {
		t.Fatalf("expected duplicate evidence error, got %v", err)
	}
}

func TestRenderAnalysisInputReplacesPlaceholder(t *testing.T) {
	input := analysisInput{Recording: map[string]any{"recording_id": 7001}}
	rendered := renderAnalysisInput("输入：{{input_json}}", input)
	if !strings.Contains(rendered, "7001") || strings.Contains(rendered, "{{") {
		t.Fatalf("unexpected render result: %s", rendered)
	}
}

func TestComputeCoverage(t *testing.T) {
	complete := recordingMeta{Duration: 500, TranscriptionStatus: "completed", EncounterRecordingCount: 1, RawFirstStart: 2, RawLastEnd: 495, HasRawTiming: true}
	if got := computeCoverage(complete, 10); got != coverageComplete {
		t.Fatalf("complete case got %s", got)
	}
	multi := complete
	multi.EncounterRecordingCount = 2
	if got := computeCoverage(multi, 10); got != coveragePartial {
		t.Fatalf("multi-recording case got %s", got)
	}
	pending := complete
	pending.TranscriptionStatus = "pending"
	if got := computeCoverage(pending, 10); got != coverageUnknown {
		t.Fatalf("pending case got %s", got)
	}
	late := complete
	late.RawLastEnd = 300
	if got := computeCoverage(late, 10); got != coverageUnknown {
		t.Fatalf("late-end case got %s", got)
	}
}

func TestMapSpeaker(t *testing.T) {
	cases := map[string]string{"consultant": "咨询师", "customer": "客户", "patient": "客户", "doctor": "咨询师", "": "未知", "客户": "客户"}
	for raw, want := range cases {
		if got := mapSpeaker(raw); got != want {
			t.Fatalf("mapSpeaker(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestAlignSegmentTimestamps(t *testing.T) {
	segments := []transcriptSegment{
		{Speaker: "咨询师", Text: "您好，请坐"},
		{Speaker: "客户", Text: "我想做全飞秒"},
	}
	raw := []rawSegment{
		{Text: "哎，您好，请坐", Start: 1.2, End: 3.0},
		{Text: "我想做全飞秒", Start: 3.5, End: 6.0},
	}
	alignSegmentTimestamps(segments, raw)
	for index, want := range []int{1200, 3500} {
		if segments[index].StartMS == nil || *segments[index].StartMS != want {
			t.Fatalf("segment %d start = %v, want %d", index, segments[index].StartMS, want)
		}
	}
}

func TestValidateDealCardRepairsCompositeQuote(t *testing.T) {
	card := baseCardV11()
	ctx := validationCtxV11()
	start2 := 20000
	ctx.Sources["s2"] = evidenceSource{ID: "s2", Text: "今天已经时间比较晚了，对我得先检查。", Speaker: "咨询师", StartMS: &start2}
	ctx.SegmentOrder = []string{"s1", "s2"}
	card["evidence_refs"] = []any{map[string]any{
		"id":         "ev_1",
		"quote":      "今天已经时间比较晚了……对我得先检查。",
		"speaker":    "咨询师",
		"source_ref": "s2",
		"start_ms":   nil,
		"used_for":   []any{"结论"},
	}}
	validated, err := validateCard(t, card, ctx)
	if err != nil {
		t.Fatalf("composite quote should be accepted: %v", err)
	}
	repaired := validated["evidence_refs"].([]any)[0].(map[string]any)
	if repaired["source_ref"] != "s2" {
		t.Fatalf("composite quote should resolve to s2, got %v", repaired["source_ref"])
	}
	if repaired["start_ms"] != float64(20000) {
		t.Fatalf("start_ms should be repaired to 20000, got %v", repaired["start_ms"])
	}
}

func TestValidateDealCardAcceptsBrief(t *testing.T) {
	card := baseCardV11()
	card["brief"] = map[string]any{
		"situation":     "客户带孩子来做近视手术，检查已完成，术式还没定。",
		"blocker":       "家长担心加做角膜交联要多花 6000 元。",
		"next_step":     "今天 18:00 前由盖珊珊找周教授确认交联是否必须。",
		"evidence_refs": []any{"ev_1"},
	}
	if _, err := validateCard(t, card, validationCtxV11()); err != nil {
		t.Fatalf("brief should be accepted: %v", err)
	}
}

func TestValidateDealCardRejectsBriefPromise(t *testing.T) {
	card := baseCardV11()
	card["brief"] = map[string]any{
		"situation":     "客户已经了解方案，准备今天决定。",
		"blocker":       nil,
		"next_step":     "今天联系客户，保证成交。",
		"evidence_refs": []any{"ev_1"},
	}
	if _, err := validateCard(t, card, validationCtxV11()); err == nil || !strings.Contains(err.Error(), "forbidden promise") {
		t.Fatalf("expected forbidden promise error, got %v", err)
	}
}

func TestValidateDealCardRejectsBriefUnknownRef(t *testing.T) {
	card := baseCardV11()
	card["brief"] = map[string]any{
		"situation":     "客户带孩子来做近视手术，检查已完成。",
		"blocker":       nil,
		"next_step":     "今天由咨询师确认最终术式。",
		"evidence_refs": []any{"ev_99"},
	}
	if _, err := validateCard(t, card, validationCtxV11()); err == nil || !strings.Contains(err.Error(), "unknown evidence ref") {
		t.Fatalf("expected unknown evidence error, got %v", err)
	}
}

func TestValidateDealCardAcceptsOtherConcernAndTrust(t *testing.T) {
	card := baseCardV11()
	decision := card["decision"].(map[string]any)
	decision["trust"].([]any)[0].(map[string]any)["trust_target"] = "其他"
	decision["trust"].([]any)[0].(map[string]any)["target_label"] = "顾问"
	decision["concerns"].([]any)[0].(map[string]any)["type"] = "其他"
	decision["concerns"].([]any)[0].(map[string]any)["type_label"] = "医保报销"
	if _, err := validateCard(t, card, validationCtxV11()); err != nil {
		t.Fatalf("other concern/trust should be accepted: %v", err)
	}
}

func TestValidateDealCardRejectsBriefWithoutConsultantName(t *testing.T) {
	card := baseCardV11()
	card["brief"] = map[string]any{
		"situation":     "客户带孩子来做近视手术，检查已完成。",
		"blocker":       nil,
		"next_step":     "今天 18:00 前确认最终术式。",
		"evidence_refs": []any{"ev_1"},
	}
	if _, err := validateCard(t, card, validationCtxV11()); err == nil || !strings.Contains(err.Error(), "name the consultant") {
		t.Fatalf("expected consultant name error, got %v", err)
	}
}

func TestValidateDealCardRejectsBriefWithoutConcreteTime(t *testing.T) {
	card := baseCardV11()
	card["brief"] = map[string]any{
		"situation":     "客户带孩子来做近视手术，检查已完成。",
		"blocker":       nil,
		"next_step":     "盖珊珊联系家属确认最终术式。",
		"evidence_refs": []any{"ev_1"},
	}
	if _, err := validateCard(t, card, validationCtxV11()); err == nil || !strings.Contains(err.Error(), "concrete time") {
		t.Fatalf("expected concrete time error, got %v", err)
	}
}
