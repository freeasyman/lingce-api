package emrdiagnosis

import "testing"

func TestValidateTCMDoesNotRequireWesternDiagnosis(t *testing.T) {
	content := map[string]any{
		"tcm_diagnosis": map[string]any{"diagnosis_name": "风寒感冒"},
	}
	if err := Validate("tcm", content, true); err != nil {
		t.Fatalf("TCM record should not require western diagnosis: %v", err)
	}
	if err := Validate("tcm", map[string]any{}, true); err == nil {
		t.Fatalf("TCM record should still require tcm_diagnosis")
	}
}
