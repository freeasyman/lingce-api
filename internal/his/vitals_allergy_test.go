package his

import (
	"strings"
	"testing"
)

func TestEnsureAllergyHistory(t *testing.T) {
	cases := []struct {
		name     string
		content  map[string]any
		patient  map[string]any
		customer string
		want     string
	}{
		{
			name:    "keeps doctor input",
			content: map[string]any{"allergy_history": "青霉素过敏"},
			patient: map[string]any{"allergy": "磺胺类过敏"},
			want:    "青霉素过敏",
		},
		{
			name:     "falls back to customer archive",
			content:  map[string]any{},
			patient:  map[string]any{"allergy": "快照旧值"},
			customer: " 青霉素过敏（皮试阳性） ",
			want:     "青霉素过敏（皮试阳性）",
		},
		{
			name:    "falls back to patient snapshot",
			content: map[string]any{},
			patient: map[string]any{"allergy": "磺胺类过敏"},
			want:    "磺胺类过敏",
		},
		{
			name:    "writes 无 when no allergy anywhere",
			content: map[string]any{},
			patient: map[string]any{},
			want:    "无",
		},
		{
			name:    "blank content is treated as missing",
			content: map[string]any{"allergy_history": "   "},
			patient: map[string]any{"allergy": "无"},
			want:    "无",
		},
	}
	for _, tc := range cases {
		ensureAllergyHistory(tc.content, tc.patient, tc.customer)
		if got := tc.content["allergy_history"]; got != tc.want {
			t.Fatalf("%s: allergy_history = %#v, want %q", tc.name, got, tc.want)
		}
	}
}

func TestValidatePhysicalExamContent(t *testing.T) {
	valid := map[string]any{
		"temperature": "37.2", "blood_pressure": "130/85", "pulse": "82",
		"respiration": "18", "blood_glucose": "5.6",
		"height": "168", "weight": "60", "bmi": "21.3", "text": "神清",
	}
	if err := validatePhysicalExamContent(valid); err != nil {
		t.Fatalf("valid vitals rejected: %v", err)
	}
	// 空值与纯文本历史数据不校验。
	if err := validatePhysicalExamContent(map[string]any{}); err != nil {
		t.Fatalf("empty vitals rejected: %v", err)
	}
	if err := validatePhysicalExamContent("T 36.9℃，BP 142/88 mmHg"); err != nil {
		t.Fatalf("legacy text rejected: %v", err)
	}

	cases := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"temperature low", "temperature", "33.9", "体温"},
		{"temperature high", "temperature", "42.1", "体温"},
		{"pulse low", "pulse", "29", "脉搏"},
		{"pulse non integer", "pulse", "82.5", "脉搏"},
		{"respiration high", "respiration", "61", "呼吸"},
		{"glucose low", "blood_glucose", "0.9", "血糖"},
		{"height low", "height", "49", "身高"},
		{"weight high", "weight", "301", "体重"},
		{"not number", "temperature", "abc", "体温"},
	}
	for _, tc := range cases {
		err := validatePhysicalExamContent(map[string]any{tc.key: tc.value})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want message containing %q", tc.name, err, tc.want)
		}
	}

	bpCases := []struct {
		name  string
		value string
		want  string
	}{
		{"format", "130-85", "收缩压/舒张压"},
		{"systolic low", "69/85", "收缩压"},
		{"systolic high", "221/85", "收缩压"},
		{"diastolic low", "130/39", "舒张压"},
		{"diastolic high", "130/141", "舒张压"},
		{"non number", "130/abc", "舒张压"},
	}
	for _, tc := range bpCases {
		err := validatePhysicalExamContent(map[string]any{"blood_pressure": tc.value})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want message containing %q", tc.name, err, tc.want)
		}
	}
}
