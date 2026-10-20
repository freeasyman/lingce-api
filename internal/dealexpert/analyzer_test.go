package dealexpert

import (
	"encoding/json"
	"strings"
	"testing"
)

const testTranscript = "客户：我想做全飞秒，但是要回去和家里人商量。\n咨询师：可以，我把方案和价格发给您。"

func baseCard() map[string]any {
	qualityItem := map[string]any{"score": 3, "evidence_refs": []any{}, "impact_on_deal": "一般"}
	return map[string]any{
		"conclusion": map[string]any{
			"current_state":   "有明确意向但未锁定下一步",
			"business_impact": "仍可推进",
			"main_basis":      []any{"客户说想回去和家里人商量"},
			"verification":    "记录是否预约",
		},
		"decision": map[string]any{
			"trust": map[string]any{
				"trust_target":  "医生",
				"state":         "部分建立",
				"summary":       "认可方案",
				"evidence_refs": []any{"ev_1"},
			},
			"need_summary":   "想解决近视",
			"concerns":       []any{map[string]any{"type": "家属或共同决策人", "summary": "要和家里人商量", "is_primary": true, "evidence_refs": []any{"ev_1"}}},
			"decision_state": "有意向但未决定",
			"condition":      nil,
			"due_basis":      "after_family_discussion",
			"evidence_refs":  []any{"ev_1"},
		},
		"product_findings": []any{map[string]any{
			"product_id":                     3,
			"product_name":                   "全飞秒Pro近视手术",
			"finding_type":                   "已正确推荐",
			"customer_evidence":              "我想做全飞秒",
			"matching_reason":                "客户明确表达需求",
			"recommended_by_consultant":      true,
			"requires_clinical_confirmation": false,
			"suggested_action":               "补发方案资料",
			"product_version":                "v1",
			"confidence":                     "high",
			"evidence_refs":                  []any{"ev_1"},
		}},
		"primary_action": map[string]any{
			"goal":                 "确认家属顾虑",
			"action":               "今天打电话确认",
			"suggested_content":    "想确认一下家人最担心什么",
			"owner_employee_id":    float64(9102),
			"due_basis":            "today",
			"source_evidence_refs": []any{"ev_1"},
		},
		"secondary_findings": []any{},
		"evidence_refs": []any{map[string]any{
			"id":       "ev_1",
			"quote":    "我想做全飞秒",
			"speaker":  "客户",
			"start_ms": float64(12000),
			"used_for": []any{"结论", "需求"},
		}},
		"quality": map[string]any{
			"需求洞察": qualityItem,
			"方案呈现": qualityItem,
			"价值塑造": qualityItem,
			"异议处理": qualityItem,
			"信任建立": qualityItem,
			"成交推动": qualityItem,
			"客户体验": qualityItem,
		},
		"confidence": "high",
		"unknowns":   []any{},
	}
}

func encodeCard(t *testing.T, card map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal card: %v", err)
	}
	return string(raw)
}

func validationContext() cardValidationContext {
	return cardValidationContext{
		Transcript:        testTranscript,
		CatalogProductIDs: map[int64]bool{3: true},
		CatalogAvailable:  true,
		OwnerEmployeeID:   9101,
	}
}

func validate(t *testing.T, card map[string]any) (map[string]any, error) {
	t.Helper()
	schema, err := loadDealCardSchema("")
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	return validateDealCard(encodeCard(t, card), schema, validationContext())
}

func TestValidateDealCardAcceptsValidCard(t *testing.T) {
	card, err := validate(t, baseCard())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	action, _ := card["primary_action"].(map[string]any)
	if action["owner_employee_id"] != int64(9101) {
		t.Fatalf("owner should be overridden to 9101, got %v", action["owner_employee_id"])
	}
}

func TestValidateDealCardAllowsNullAction(t *testing.T) {
	card := baseCard()
	card["primary_action"] = nil
	if _, err := validate(t, card); err != nil {
		t.Fatalf("null primary_action should be allowed: %v", err)
	}
}

func TestValidateDealCardRejectsUnknownField(t *testing.T) {
	card := baseCard()
	card["extra_field"] = "x"
	if _, err := validate(t, card); err == nil || !strings.Contains(err.Error(), "additional") {
		t.Fatalf("expected additionalProperties error, got %v", err)
	}
}

func TestValidateDealCardRejectsLowConfidenceAction(t *testing.T) {
	card := baseCard()
	card["confidence"] = "low"
	if _, err := validate(t, card); err == nil || !strings.Contains(err.Error(), "low confidence") {
		t.Fatalf("expected low-confidence rejection, got %v", err)
	}
}

func TestValidateDealCardRejectsUnknownEvidenceRef(t *testing.T) {
	card := baseCard()
	decision := card["decision"].(map[string]any)
	decision["evidence_refs"] = []any{"ev_9"}
	if _, err := validate(t, card); err == nil || !strings.Contains(err.Error(), "unknown evidence ref") {
		t.Fatalf("expected unknown evidence error, got %v", err)
	}
}

func TestValidateDealCardRejectsQuoteNotInTranscript(t *testing.T) {
	card := baseCard()
	card["evidence_refs"] = []any{map[string]any{
		"id": "ev_1", "quote": "客户说一定要做", "speaker": "客户",
		"start_ms": nil, "used_for": []any{"结论"},
	}}
	if _, err := validate(t, card); err == nil || !strings.Contains(err.Error(), "not found in transcript") {
		t.Fatalf("expected quote mismatch error, got %v", err)
	}
}

func TestValidateDealCardRejectsProductGating(t *testing.T) {
	card := baseCard()
	findings := card["product_findings"].([]any)
	finding := findings[0].(map[string]any)
	finding["finding_type"] = "可能漏推"
	if _, err := validate(t, card); err == nil || !strings.Contains(err.Error(), "not allowed without product catalog") {
		// catalog 可用时允许；再验证目录不可用的情况
		t.Logf("catalog available: %v", err)
	}
	ctx := validationContext()
	ctx.CatalogAvailable = false
	schema, _ := loadDealCardSchema("")
	if _, err := validateDealCard(encodeCard(t, card), schema, ctx); err == nil || !strings.Contains(err.Error(), "not allowed without product catalog") {
		t.Fatalf("expected product gating error, got %v", err)
	}
}

func TestValidateDealCardRequiresClinicalConfirmationForCombined(t *testing.T) {
	card := baseCard()
	finding := card["product_findings"].([]any)[0].(map[string]any)
	finding["finding_type"] = "联合机会"
	finding["requires_clinical_confirmation"] = false
	if _, err := validate(t, card); err == nil || !strings.Contains(err.Error(), "clinical confirmation") {
		t.Fatalf("expected clinical confirmation error, got %v", err)
	}
}

func TestRenderAnalysisInputWithJSONPlaceholder(t *testing.T) {
	input := map[string]any{"recording": map[string]any{"recording_id": 7001}}
	rendered := renderAnalysisInput("输入：{{input_json}}", input, "", nil, nil)
	if !strings.Contains(rendered, "7001") || strings.Contains(rendered, "{{") {
		t.Fatalf("unexpected render result: %s", rendered)
	}
}

func TestRenderAnalysisInputWithLegacyPlaceholders(t *testing.T) {
	rendered := renderAnalysisInput("转写：{{transcript}}", nil, "客户说再看看", nil, nil)
	if !strings.Contains(rendered, "客户说再看看") {
		t.Fatalf("legacy placeholder not replaced: %s", rendered)
	}
}

func TestDecodeTranscriptSegments(t *testing.T) {
	payload := []any{
		map[string]any{"speaker_role": "客户", "text": "我想做全飞秒", "start_ms": float64(1000)},
		map[string]any{"speaker": "咨询师", "text": "可以", "start_time": float64(2000)},
	}
	segments := decodeTranscriptSegments(payload, 0)
	if len(segments) != 2 || segments[0].Speaker != "客户" || segments[1].StartMS == nil || *segments[1].StartMS != 2000 {
		t.Fatalf("unexpected segments: %+v", segments)
	}
	if !strings.Contains(segmentsTranscript(segments), "客户：我想做全飞秒") {
		t.Fatalf("unexpected transcript: %s", segmentsTranscript(segments))
	}
}

func TestMapSpeaker(t *testing.T) {
	cases := map[string]string{
		"consultant": "咨询师",
		"customer":   "客户",
		"patient":    "客户",
		"doctor":     "咨询师",
		"":           "未知",
		"咨询师":        "咨询师",
		"客户":         "客户",
	}
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
		{Speaker: "咨询师", Text: "我先给您查一下角膜厚度"},
	}
	raw := []rawSegment{
		{Text: "哎，您好，请坐", Start: 1.2},
		{Text: "我想做全飞秒", Start: 3.5},
		{Text: "我先给您查一下角膜厚度", Start: 8.0},
	}
	alignSegmentTimestamps(segments, raw)
	for index, want := range []int{1200, 3500, 8000} {
		if segments[index].StartMS == nil || *segments[index].StartMS != want {
			t.Fatalf("segment %d start = %v, want %d", index, segments[index].StartMS, want)
		}
	}
}
