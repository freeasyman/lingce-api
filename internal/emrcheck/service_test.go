package emrcheck

import "testing"

func TestNormalizeQualityEvidenceAllowsEmptyExcerptForMissingField(t *testing.T) {
	items, text, err := normalizeQualityEvidence("emr.past_history.chronic", []qualityEvidence{{
		FieldCode:    "past_history",
		EvidenceType: "缺失",
		Explanation:  "既往史栏位为空，无法确认慢性病史。",
	}}, map[string]string{"past_history": "既往史"})
	if err != nil {
		t.Fatalf("normalize missing evidence: %v", err)
	}
	if len(items) != 1 || text == "" {
		t.Fatalf("normalized missing evidence = %#v, %q", items, text)
	}
	item := items[0].(map[string]any)
	if item["field_value_excerpt"] != "" {
		t.Fatalf("missing field excerpt = %#v, want empty string", item["field_value_excerpt"])
	}
}

func TestNormalizeQualityEvidenceRequiresExcerptForSupportingEvidence(t *testing.T) {
	_, _, err := normalizeQualityEvidence("emr.diagnosis.present", []qualityEvidence{{
		FieldCode:    "diagnosis",
		EvidenceType: "支持",
		Explanation:  "诊断栏位已填写。",
	}}, map[string]string{"diagnosis": "诊断"})
	if err == nil {
		t.Fatal("expected supporting evidence without excerpt to fail")
	}
}
