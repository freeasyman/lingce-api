package externaldata

import "testing"

func TestMapEncounterVisitType(t *testing.T) {
	cases := map[string]string{
		"初诊":           "first_visit",
		"复诊":           "return_visit",
		"consultation": "consultation",
		"随访":           "follow_up",
	}
	for input, want := range cases {
		got, err := mapEncounterVisitType(input)
		if err != nil || got != want {
			t.Fatalf("mapEncounterVisitType(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := mapEncounterVisitType("未知"); err == nil {
		t.Fatal("unknown visit type should be rejected")
	}
}

func TestValidateEnvelopeObjectSpecificFields(t *testing.T) {
	base := Envelope{
		SourceSystem: "shanghai_his", SourceRecordID: "v-1", EventType: "visit.upsert",
		IdempotencyKey: "k-1", Patient: map[string]any{"source_patient_id": "p-1"},
		Data: map[string]any{"source_visit_id": "v-1", "visit_at": "2026-01-01T00:00:00Z", "doctor": "医生", "department": "科室", "chief_complaint": "主诉", "diagnosis": "诊断"},
	}
	if err := validateEnvelope("visits", base); err != nil {
		t.Fatalf("valid visit rejected: %v", err)
	}
	base.EventType = "patient.upsert"
	if err := validateEnvelope("visits", base); err == nil {
		t.Fatal("mismatched event type should be rejected")
	}
	base.EventType = "visit.upsert"
	delete(base.Data, "chief_complaint")
	if err := validateEnvelope("visits", base); err != nil {
		t.Fatalf("missing clinical field should still be accepted: %v", err)
	}
}
