package dealexpert

import (
	"encoding/json"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/pipeline"
)

// v210 适配层测试：只验证 deal-card-v2.1 → deal-card-v1.1 的结构映射，
// 不重复阶段 4 的语义评测，也不把适配成功当成客户结论正确。

func v210TestEvidence(id, speaker, sourceRef, quote string) map[string]any {
	return map[string]any{
		"id": id, "quote": quote, "speaker": speaker, "source_ref": sourceRef,
		"start_ms": float64(1200), "used_for": []any{"结论"},
		"source_kind": "transcript", "timing_basis": "aligned", "role_basis": "cleaned_transcription",
	}
}

func v210TestQualityItem(assessment string, score any, withRefs bool) map[string]any {
	refs := []any{}
	if withRefs {
		refs = []any{"ev_1"}
	}
	return map[string]any{
		"assessment": assessment, "score": score, "comment": "维度说明",
		"evidence_refs": refs, "impact_on_deal": "对成交有影响", "highlight_narrative": "可复用做法",
	}
}

func v210TestQuality() map[string]any {
	dimensions := map[string]any{}
	for _, name := range v11QualityNames {
		dimensions[name] = v210TestQualityItem("scored", float64(4), true)
	}
	dimensions["异议化解"] = v210TestQualityItem("scored", float64(2), true)
	dimensions["促成与收尾"] = v210TestQualityItem("insufficient_evidence", nil, false)
	return map[string]any{"standard_version": "compass-sales-v1.1", "dimensions": dimensions}
}

func v210TestDecision(state string) map[string]any {
	return map[string]any{
		"decision_state": state, "state_basis": "transcript_judgment", "evidence_refs": []any{"ev_1"},
		"condition": "等家里人商量后再定", "condition_due_quote": "我回去和我老公商量一下",
		"condition_kind": "family_discussion", "condition_evidence_refs": []any{"ev_2"},
		"trust": []any{map[string]any{
			"trust_target": "医生", "state": "部分建立", "summary": "认可医生的解释",
			"evidence_refs": []any{"ev_2"}, "target_label": nil,
		}},
		"need_state": "明确", "need_summary": "想解决看东西模糊", "need_evidence_refs": []any{"ev_3"},
		"concerns": []any{
			map[string]any{"type": "家属或共同决策人", "type_label": nil, "summary": "要和家里人商量", "is_primary": true, "evidence_refs": []any{"ev_2"}},
			map[string]any{"type": "价格与价值", "type_label": nil, "summary": "担心价格", "is_primary": false, "evidence_refs": []any{"ev_4"}},
		},
		"outcome_signals": []any{},
		"target":          "全飞秒", "confidence": map[string]any{"level": "medium", "scope": "scheme"},
	}
}

func v210TestCard() map[string]any {
	secondary := v210TestDecision("有意向但未决定")
	secondary["target"] = "ICL"
	return map[string]any{
		"schema_version":    "deal-card-v2.1",
		"rules_version":     "deal-compiler-v2.1.0-rc11",
		"status":            "analyzed",
		"input_snapshot_id": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"conclusion": map[string]any{
			"current_state": "当前录音判断：暂缓。", "business_impact": "仍可推进",
			"main_basis": []any{"ev_1"}, "verification": "核对客户当前意向、原文依据和已核实业务事件。",
		},
		"decision":         v210TestDecision("暂缓"),
		"scheme_decisions": []any{v210TestDecision("暂缓"), secondary},
		"clinical_conflict": map[string]any{
			"active": false, "reason": nil,
			"evidence_refs": []any{}, "assessment": "no_unresolved_issue_observed",
		},
		"contact_policy": "allow",
		"product_findings": []any{map[string]any{
			"product_id": float64(3), "product_name": "全飞秒", "finding_type": "已正确推荐",
			"customer_evidence": "客户提到想摘镜", "matching_reason": "目录匹配",
			"recommended_by_consultant": true, "requires_clinical_confirmation": true,
			"suggested_action": nil, "product_version": "1", "confidence": "high",
			"evidence_refs": []any{"ev_3"},
		}},
		"primary_action": map[string]any{
			"type": "sales_follow_up", "template": "confirm_next_step",
			"goal":              "请专业人员核实录音中的未解决事项，不预设诊断或治疗方案。",
			"action":            "请专业人员核实录音中的未解决事项，不预设诊断或治疗方案。",
			"suggested_content": nil, "owner_employee_id": nil, "due_basis": "within_48h",
			"due_provenance": "system_suggestion", "due_quote": nil, "timing_evidence_refs": []any{},
			"source_evidence_refs": []any{"ev_1"}, "customer_window": nil,
		},
		"secondary_findings": []any{
			map[string]any{"summary": "员工陈述：晶体植入被 pass。", "evidence_refs": []any{"ev_5"}},
		},
		"evidence_refs": []any{
			v210TestEvidence("ev_1", "客户", "s1", "我先回去想想"),
			v210TestEvidence("ev_2", "客户", "s2", "我回去和我老公商量一下"),
			v210TestEvidence("ev_3", "客户", "s3", "我想摘掉眼镜"),
			v210TestEvidence("ev_4", "客户", "s4", "这个价格有点贵"),
			v210TestEvidence("ev_5", "咨询师", "s5", "晶体植入我们先不考虑"),
		},
		"quality": v210TestQuality(),
		"coach": map[string]any{
			"observations": []any{map[string]any{
				"behavior":          "请澄清客户当前问题和所讨论的方案。",
				"customer_response": nil, "evidence_refs": []any{"ev_1"}, "response_evidence_refs": []any{},
			}},
			"improvement": map[string]any{
				"suggestion": "请客户复述理解，并核对仍需解释的问题。", "evidence_refs": []any{"ev_2"},
			},
		},
		"confidence": "medium",
		"unknowns":   []any{"客户最终选择哪个方案未知"},
		"brief": map[string]any{
			"situation": "客户暂缓，需要先和家里人商量。", "blocker": nil,
			"next_step": "先由专业人员核实未解决事项，再决定后续处理。", "evidence_refs": []any{"ev_1"},
		},
		"recording_observations": []any{
			map[string]any{"kind": "condition", "summary": "客户要先和家里人商量", "target": "全飞秒", "subject_binding": "target_customer", "evidence_refs": []any{"ev_2"}},
			map[string]any{"kind": "need", "summary": "想摘镜", "target": "全飞秒", "subject_binding": "target_customer", "evidence_refs": []any{"ev_3"}},
		},
		"event_reports": []any{
			map[string]any{"event_type": "预约", "state": "planned", "subject": "target_customer", "evidence_refs": []any{"ev_1"}},
			map[string]any{"event_type": "付款", "state": "denied", "subject": "target_customer", "evidence_refs": []any{"ev_2"}},
		},
		"analysis_scope":        map[string]any{"kind": "business", "label": "医疗/销售咨询录音。", "evidence_refs": []any{"ev_1"}},
		"target_binding":        map[string]any{"state": "single_customer", "target_customer_id": float64(11), "binding_record_id": "deal_expert_analyses:1:customer_id"},
		"business_facts":        []any{},
		"contact_evidence_refs": []any{},
		"intent":                map[string]any{"state": "暂缓", "target": "全飞秒", "scope": "recording", "evidence_refs": []any{"ev_1"}},
	}
}

func mustAdaptV210(t *testing.T, card map[string]any) map[string]any {
	t.Helper()
	adapted, err := adaptV210Card(card)
	if err != nil {
		t.Fatalf("adapt v210 card: %v", err)
	}
	if err := validateAdaptedCard(adapted); err != nil {
		raw, _ := json.Marshal(adapted)
		t.Fatalf("adapted card invalid: %v\n%s", err, raw)
	}
	return adapted
}

func TestAdaptV210CardKeepsV11DisplayContract(t *testing.T) {
	adapted := mustAdaptV210(t, v210TestCard())
	if adapted["schema_version"] != dealCardVersion {
		t.Fatalf("schema_version = %v, want %s", adapted["schema_version"], dealCardVersion)
	}
	if _, ok := adapted["scheme_decisions"]; ok {
		t.Fatal("v2.1-only field leaked into v1.1 card")
	}
	if _, ok := adapted["recording_observations"]; ok {
		t.Fatal("recording_observations leaked into v1.1 card")
	}
	decision := adapted["decision"].(map[string]any)
	if decision["decision_state"] != "暂缓" || decision["condition"] != "等家里人商量后再定" {
		t.Fatalf("decision not mapped: %v", decision)
	}
	if got := len(decision["trust"].([]any)); got != 1 {
		t.Fatalf("trust items = %d, want 1", got)
	}
	if decision["need_summary"] != "想解决看东西模糊" {
		t.Fatalf("need_summary = %v", decision["need_summary"])
	}
	concerns := decision["concerns"].([]any)
	if len(concerns) != 2 || concerns[0].(map[string]any)["is_primary"] != true {
		t.Fatalf("concerns not mapped: %v", concerns)
	}
	signals := decision["outcome_signals"].([]any)
	if len(signals) != 1 || signals[0].(map[string]any)["type"] != "预约" {
		t.Fatalf("outcome_signals should only carry planned events: %v", signals)
	}

	evidence := adapted["evidence_refs"].([]any)
	first := evidence[0].(map[string]any)
	for _, key := range []string{"source_kind", "timing_basis", "role_basis"} {
		if _, ok := first[key]; ok {
			t.Fatalf("v2.1 evidence field %q leaked", key)
		}
	}
	if _, ok := first["start_ms"].(int64); !ok {
		t.Fatalf("start_ms must be an integer, got %T", first["start_ms"])
	}

	action := adapted["primary_action"].(map[string]any)
	if action["suggested_content"] == "" || action["due_basis"] != "within_48h" || action["owner_employee_id"] != nil {
		t.Fatalf("action not mapped: %v", action)
	}
	if _, ok := action["template"]; ok {
		t.Fatal("action template leaked into v1.1 card")
	}

	coach := adapted["coach"].(map[string]any)
	practices := coach["successful_practices"].([]any)
	if len(practices) != 1 || practices[0].(map[string]any)["reusable_when"] == "" {
		t.Fatalf("coach practices not mapped: %v", practices)
	}
	if coach["refund_review"] != nil {
		t.Fatal("refund_review must stay null without a v2.1 source")
	}

	brief := adapted["brief"].(map[string]any)
	if brief["blocker"] != "要和家里人商量" {
		t.Fatalf("blocker should fall back to the primary concern, got %v", brief["blocker"])
	}
	if got := len(brief["evidence_refs"].([]any)); got < 1 || got > 4 {
		t.Fatalf("brief evidence_refs = %d, want 1..4", got)
	}

	secondary := adapted["secondary_findings"].([]any)
	joined := ""
	for _, item := range secondary {
		joined += item.(map[string]any)["summary"].(string) + "\n"
	}
	if !containsText(joined, "其他方案") || !containsText(joined, "客户否认已发生") {
		t.Fatalf("secondary findings lost scheme/event info: %s", joined)
	}
}

func TestAdaptV210CardQualityNeverFakesScores(t *testing.T) {
	adapted := mustAdaptV210(t, v210TestCard())
	dimensions := adapted["quality"].(map[string]any)["dimensions"].(map[string]any)
	if item := dimensions["异议化解"].(map[string]any); item["highlight_narrative"] != nil {
		t.Fatalf("score<=3 must clear highlight_narrative: %v", item)
	}
	if item := dimensions["促成与收尾"].(map[string]any); item["assessment"] != "insufficient_evidence" || item["score"] != nil {
		t.Fatalf("insufficient evidence must stay unscored: %v", item)
	}
	if count := len(dimensions); count != 7 {
		t.Fatalf("quality dimensions = %d, want 7", count)
	}
}

func TestAdaptV210CardUnresolvedBindingStaysConservative(t *testing.T) {
	card := v210TestCard()
	card["decision"] = map[string]any{
		"decision_state": "无法判断", "state_basis": "unknown", "evidence_refs": []any{},
		"condition": nil, "condition_due_quote": nil, "trust": []any{},
		"need_state": "无法判断", "need_summary": nil, "need_evidence_refs": []any{},
		"concerns": []any{}, "outcome_signals": []any{},
		"target": "全飞秒", "confidence": map[string]any{"level": "unknown", "scope": "scheme"},
	}
	card["scheme_decisions"] = []any{card["decision"]}
	card["primary_action"] = nil
	card["contact_policy"] = "review"
	card["target_binding"] = map[string]any{"state": "unresolved", "target_customer_id": nil, "binding_record_id": nil}
	card["brief"] = map[string]any{
		"situation": "当前录音判断：无法判断。关键事项待人工复核。", "blocker": nil,
		"next_step": "先由人工复核关键事项，再决定后续处理。", "evidence_refs": []any{},
	}
	card["conclusion"] = map[string]any{
		"current_state": "当前录音判断：无法判断。关键事项待人工复核。", "business_impact": "无法判断",
		"main_basis": []any{}, "verification": "请人工核对关键判断、来源缺口和业务事实后再决定后续处理。",
	}
	card["recording_observations"] = []any{
		map[string]any{"kind": "need", "summary": "想摘镜", "target": "全飞秒", "subject_binding": "unresolved", "evidence_refs": []any{"ev_3"}},
	}
	adapted := mustAdaptV210(t, card)
	decision := adapted["decision"].(map[string]any)
	if decision["decision_state"] != "无法判断" || decision["need_summary"] != nil {
		t.Fatalf("unresolved binding must not publish personal conclusions: %v", decision)
	}
	if len(decision["concerns"].([]any)) != 0 || len(decision["trust"].([]any)) != 0 {
		t.Fatalf("unresolved binding must keep concerns/trust empty")
	}
	if adapted["primary_action"] != nil {
		t.Fatalf("unresolved binding must not publish an action: %v", adapted["primary_action"])
	}
	// 没有可引用证据时不下发 brief，前端回落到结论与动作。
	if brief, ok := adapted["brief"]; ok && brief != nil {
		t.Fatalf("brief without evidence refs must be omitted, got %v", brief)
	}
	joined := ""
	for _, item := range adapted["secondary_findings"].([]any) {
		joined += item.(map[string]any)["summary"].(string) + "\n"
	}
	if !containsText(joined, "录音级需求观察（待核实）") {
		t.Fatalf("recording-level observation should be preserved: %s", joined)
	}
}

func TestAdaptV210CardRejectsNonV210Schema(t *testing.T) {
	if _, err := adaptV210Card(map[string]any{"schema_version": dealCardVersion}); err == nil {
		t.Fatal("expected schema mismatch error")
	}
	if _, err := adaptV210Card(nil); err == nil {
		t.Fatal("expected empty card error")
	}
}

func TestAdaptV210CardDemotesPostponedWithoutCondition(t *testing.T) {
	card := v210TestCard()
	decision := v210TestDecision("暂缓")
	decision["condition"] = nil
	decision["condition_due_quote"] = nil
	card["decision"] = decision
	card["scheme_decisions"] = []any{decision}
	adapted := mustAdaptV210(t, card)
	got := adapted["decision"].(map[string]any)
	if got["decision_state"] != "无法判断" || got["condition"] != nil {
		t.Fatalf("postponed without condition must be demoted: %v", got)
	}
}

func TestNormalizeAnalysisVersion(t *testing.T) {
	cases := map[string]string{
		"":                          AnalysisVersionV200,
		"v200":                      AnalysisVersionV200,
		"deal-compiler-v2.0.0":      AnalysisVersionV200,
		"v210-rc11":                 AnalysisVersionV210RC11,
		" RC11 ":                    AnalysisVersionV210RC11,
		"deal-compiler-v2.1.0-rc11": AnalysisVersionV210RC11,
	}
	for input, want := range cases {
		got, err := normalizeAnalysisVersion(input)
		if err != nil {
			t.Fatalf("normalize(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("normalize(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := normalizeAnalysisVersion("rc9"); err == nil {
		t.Fatal("unknown version must not silently fall back")
	}
}

func TestV210OwnershipBinding(t *testing.T) {
	customerID := int64(42)
	item := &pendingAnalysis{ID: 7, RoleCode: "consultant", CustomerID: &customerID}
	material := pipeline.InputMaterial{
		Transcript: []pipeline.TranscriptInput{
			{ID: "s1", Speaker: "客户"},
			{ID: "s2", Speaker: "咨询师"},
			{ID: "s3", Speaker: "未知"},
			{ID: "s4", Speaker: "医生"},
		},
		Facts: []pipeline.BusinessFactInput{{ID: "f1", Type: "预约"}},
	}
	evidence := v210Ownership(item, material)
	if evidence.State != "single_customer" || evidence.TargetCustomerID == nil || *evidence.TargetCustomerID != 42 {
		t.Fatalf("binding not resolved: %+v", evidence)
	}
	if !evidence.ConsultantRoleVerified {
		t.Fatal("consultant role should follow the server role code")
	}
	if evidence.Sources["s1"].Subject != "target_customer" || evidence.Sources["s1"].SpeakerID == nil {
		t.Fatalf("customer source not bound: %+v", evidence.Sources["s1"])
	}
	if evidence.Sources["s2"].Subject != "consultant" {
		t.Fatalf("consultant source not bound: %+v", evidence.Sources["s2"])
	}
	if evidence.Sources["s3"].Subject != "unresolved" || evidence.Sources["s3"].SpeakerID != nil {
		t.Fatalf("unknown speaker must stay unresolved: %+v", evidence.Sources["s3"])
	}
	if evidence.Sources["s4"].Subject != "other_person" {
		t.Fatalf("doctor source should be other_person: %+v", evidence.Sources["s4"])
	}
	if evidence.FactCustomers["f1"] != 42 {
		t.Fatalf("verified fact not bound to customer: %+v", evidence.FactCustomers)
	}

	unbound := v210Ownership(&pendingAnalysis{ID: 8, RoleCode: "consultant"}, material)
	if unbound.State != "unresolved" || unbound.TargetCustomerID != nil || unbound.BindingRecordID != nil {
		t.Fatalf("unbound analysis must stay unresolved: %+v", unbound)
	}
	for id, source := range unbound.Sources {
		if source.Subject != "unresolved" || source.SpeakerID != nil {
			t.Fatalf("source %s must stay unresolved: %+v", id, source)
		}
	}
	if len(unbound.FactCustomers) != 0 {
		t.Fatalf("unbound analysis must not claim business facts: %+v", unbound.FactCustomers)
	}
}

func containsText(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestAdaptV210CardClinicalHoldStripsSalesContent(t *testing.T) {
	card := v210TestCard()
	card["clinical_conflict"] = map[string]any{
		"active": true, "reason": "录音中存在待专业核实的事项。",
		"evidence_refs": []any{"ev_1"}, "assessment": "unresolved",
	}
	// v2.1 正常不会给出 allow，但适配层必须把展示契约的不变量收敛掉。
	card["contact_policy"] = "allow"
	adapted := mustAdaptV210(t, card)
	if adapted["contact_policy"] != "pause_clinical" {
		t.Fatalf("clinical hold must force pause_clinical, got %v", adapted["contact_policy"])
	}
	if got := len(adapted["product_findings"].([]any)); got != 0 {
		t.Fatalf("clinical hold must remove product findings, got %d", got)
	}
	if adapted["primary_action"] != nil {
		t.Fatalf("clinical hold must remove sales action, got %v", adapted["primary_action"])
	}
}

func TestAdaptV210CardReviewPolicyAndLowConfidenceDropAction(t *testing.T) {
	review := v210TestCard()
	review["contact_policy"] = "review"
	if adapted := mustAdaptV210(t, review); adapted["primary_action"] != nil {
		t.Fatalf("review policy must not publish an action: %v", adapted["primary_action"])
	}
	low := v210TestCard()
	low["confidence"] = "low"
	if adapted := mustAdaptV210(t, low); adapted["primary_action"] != nil {
		t.Fatalf("low confidence must not publish an action: %v", adapted["primary_action"])
	}
}

func TestNewAnalyzerDefaultsToV200AndRejectsUnknownVersion(t *testing.T) {
	if got := NewAnalyzer(nil, nil).AnalysisVersion(); got != AnalysisVersionV200 {
		t.Fatalf("default analyzer version = %q, want %q", got, AnalysisVersionV200)
	}
	analyzer, err := NewAnalyzerWithVersion(nil, nil, "v210-rc11")
	if err != nil || analyzer.AnalysisVersion() != AnalysisVersionV210RC11 {
		t.Fatalf("explicit rc11 analyzer not selected: %v %q", err, analyzer.AnalysisVersion())
	}
	if _, err := NewAnalyzerWithVersion(nil, nil, "rc9"); err == nil {
		t.Fatal("unsupported version must be rejected at startup")
	}
}
