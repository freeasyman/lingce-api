package emrdiagnosis

import "testing"

func TestValidateDualDiagnosisContract(t *testing.T) {
	tests := []struct {
		name, template string
		content        map[string]any
		complete       bool
		wantErr        bool
	}{
		{"western draft", "internal_medicine", map[string]any{"diagnosis": map[string]any{"diagnosis_name": "失眠", "icd10_code": "F51.0", "basis": "主诉"}}, false, false},
		{"western complete", "internal_medicine", map[string]any{"diagnosis": map[string]any{"diagnosis_name": "失眠", "icd10_code": "F51.0", "basis": "主诉"}}, true, false},
		{"tcm complete", "tcm", map[string]any{"diagnosis": map[string]any{"diagnosis_name": "失眠", "icd10_code": "F51.0", "basis": "主诉"}, "tcm_diagnosis": map[string]any{"diagnosis_name": "不寐", "pattern_name": "心脾两虚", "basis": "四诊"}}, true, false},
		{"tcm western optional", "tcm", map[string]any{"tcm_diagnosis": map[string]any{"diagnosis_name": "不寐"}}, true, false},
		{"tcm missing tcm", "tcm", map[string]any{"diagnosis": map[string]any{"diagnosis_name": "失眠"}}, true, true},
		{"basis is not name", "tcm", map[string]any{"diagnosis": map[string]any{"basis": "主诉"}, "tcm_diagnosis": map[string]any{"pattern_name": "气虚"}}, true, true},
		{"normal cannot contain tcm", "internal_medicine", map[string]any{"diagnosis": map[string]any{"diagnosis_name": "失眠"}, "tcm_diagnosis": map[string]any{}}, false, true},
		{"legacy text rejected", "internal_medicine", map[string]any{"diagnosis": map[string]any{"text": "失眠"}}, false, true},
		{"tcm icd rejected", "tcm", map[string]any{"diagnosis": map[string]any{"diagnosis_name": "失眠"}, "tcm_diagnosis": map[string]any{"diagnosis_name": "不寐", "icd10_code": "F51.0"}}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate(tt.template, tt.content, tt.complete); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
