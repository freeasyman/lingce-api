package emrdiagnosis

import "testing"

func TestDisplayPrefersStructuredDiagnosisWithICD(t *testing.T) {
	content := map[string]any{
		"diagnosis": map[string]any{"diagnosis_name": "急性支气管炎", "icd10_code": "J20.900"},
		"diagnoses": []any{map[string]any{"diagnosis_name": "旧诊断", "icd10_code": "J00"}},
	}
	if got := Display(content); got != "急性支气管炎（ICD-10：J20.900）" {
		t.Fatalf("unexpected diagnosis text: %q", got)
	}
}

func TestDisplayFallsBackToDiagnosesArrayAndTCM(t *testing.T) {
	array := map[string]any{
		"diagnoses": []any{
			map[string]any{"name": "高血压", "code": "I10"},
			"2型糖尿病",
		},
	}
	if got := Display(array); got != "高血压（ICD-10：I10）、2型糖尿病" {
		t.Fatalf("unexpected diagnoses array text: %q", got)
	}
	tcm := map[string]any{"tcm_diagnosis": map[string]any{"diagnosis_name": "月经过少", "pattern_name": "气血两虚证"}}
	if got := Display(tcm); got != "月经过少" {
		t.Fatalf("unexpected tcm diagnosis text: %q", got)
	}
	if got := Display(map[string]any{"diagnosis": ""}); got != "" {
		t.Fatalf("empty diagnosis should stay empty: %q", got)
	}
}
