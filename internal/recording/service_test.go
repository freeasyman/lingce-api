package recording

import "testing"

func TestCompactRecordingListAnalysisResultDropsHeavyFields(t *testing.T) {
	source := map[string]interface{}{
		"summary":              "summary text",
		"conversation_summary": "conversation summary",
		"status_summary":       "status summary",
		"relationship_frame":   "relationship frame",
		"scene_type_label":     "复购型",
		"intent_amount":        "1000",
		"intent_project":       "项目A",
		"visit_outcome":        "已成交",
		"decision_status":      "已成交",
		"visit_outcome_status": "closed_won",
		"deal_outcome": map[string]interface{}{
			"foo": "bar",
		},
		"persuasive": map[string]interface{}{
			"baz": "qux",
		},
		"doctor_patient_view": map[string]interface{}{
			"relationship_frame": "doctor-patient frame",
			"summary":            "dpv summary",
		},
	}

	got := compactRecordingListAnalysisResult(source)
	if got == nil {
		t.Fatalf("expected compact result")
	}
	if _, ok := got["deal_outcome"]; ok {
		t.Fatalf("deal_outcome should be dropped from list payload")
	}
	if _, ok := got["persuasive"]; ok {
		t.Fatalf("persuasive should be dropped from list payload")
	}
	if got["scene_type_label"] != "复购型" {
		t.Fatalf("unexpected scene_type_label: %#v", got["scene_type_label"])
	}
	if got["relationship_frame"] != "relationship frame" {
		t.Fatalf("unexpected relationship_frame: %#v", got["relationship_frame"])
	}
}
