package emrquality

import (
	"encoding/json"
	"testing"
)

func TestEvidenceListUnmarshalShapes(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
		first   string
	}{
		{name: "null", raw: `null`, want: 0},
		{name: "array", raw: `[{"source":"record.content","path":"a","excerpt":"x"},{"source":"record.content","path":"b","excerpt":"y"}]`, want: 2, first: "record.content"},
		{name: "single object", raw: `{"source":"record.content","path":"a","excerpt":"x"}`, want: 1, first: "record.content"},
		{name: "json string array", raw: `"[{\"source\":\"record.content\",\"path\":\"diagnosis\",\"excerpt\":\"原发性高血压\"}]"`, want: 1, first: "record.content"},
		{name: "json string object", raw: `"{\"source\":\"record.patient_snapshot\",\"path\":\"patient.gender\",\"excerpt\":\"男\"}"`, want: 1, first: "record.patient_snapshot"},
		{name: "plain text", raw: `"诊断与主诉不一致"`, want: 1, first: "llm"},
		{name: "empty string", raw: `""`, want: 0},
		{name: "unsupported number", raw: `123`, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var list evidenceList
			err := json.Unmarshal([]byte(tc.raw), &list)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(list) != tc.want {
				t.Fatalf("want %d items, got %d", tc.want, len(list))
			}
			if tc.first != "" && list[0].Source != tc.first {
				t.Fatalf("want first source %q, got %q", tc.first, list[0].Source)
			}
		})
	}
}

// 实测 qwen-max 会在 DX-002 / TRT-002 上把 evidence 数组再包成字符串，
// 这里固化该响应形态，保证整单质检不会因此失败。
func TestSemanticResponseAcceptsStringEvidence(t *testing.T) {
	raw := `{
	  "check_completed": true,
	  "checked_rule_codes": ["DX-002"],
	  "results": [{
	    "rule_code": "DX-002",
	    "result": "problem",
	    "evidence": "[{\"source\":\"record.content\",\"path\":\"diagnosis.diagnosis_name\",\"excerpt\":\"原发性高血压\"},{\"source\":\"record.content\",\"path\":\"chief_complaint\",\"excerpt\":\"头痛三天\"}]",
	    "reason": "诊断与主诉不一致",
	    "manual_review": true
	  }]
	}`

	var response semanticResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatalf("semanticResponse should tolerate string evidence, got: %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("want 1 result, got %d", len(response.Results))
	}
	got := response.Results[0]
	if got.RuleCode != "DX-002" || got.Result != "problem" {
		t.Fatalf("unexpected finding: %+v", got)
	}
	if len(got.Evidence) != 2 {
		t.Fatalf("want 2 evidence items, got %d", len(got.Evidence))
	}
	if got.Evidence[0].Path != "diagnosis.diagnosis_name" {
		t.Fatalf("unexpected evidence path: %q", got.Evidence[0].Path)
	}
}

func TestStringListUnmarshalShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "array", raw: `["a","b"]`, want: []string{"a", "b"}},
		{name: "comma separated", raw: `"原发性高血压, 头痛三天"`, want: []string{"原发性高血压", "头痛三天"}},
		{name: "single value", raw: `"gender"`, want: []string{"gender"}},
		{name: "json array string", raw: `"[\"x\",\"y\"]"`, want: []string{"x", "y"}},
		{name: "null", raw: `null`, want: nil},
		{name: "empty string", raw: `""`, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var list stringList
			if err := json.Unmarshal([]byte(tc.raw), &list); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(list) != len(tc.want) {
				t.Fatalf("want %v, got %v", tc.want, []string(list))
			}
			for i := range tc.want {
				if list[i] != tc.want[i] {
					t.Fatalf("want %v, got %v", tc.want, []string(list))
				}
			}
		})
	}
}

// 实测 qwen-max 会把 defect_keys 返回成逗号分隔字符串，整单解析不能因此失败。
func TestSemanticResponseAcceptsStringDefectKeys(t *testing.T) {
	raw := `{
	  "check_completed": true,
	  "checked_rule_codes": ["DX-002"],
	  "results": [{
	    "rule_code": "DX-002",
	    "result": "problem",
	    "evidence": null,
	    "reason": "诊断与主诉不一致",
	    "defect_keys": "原发性高血压, 头痛三天",
	    "manual_review": false
	  }]
	}`
	var response semanticResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatalf("should tolerate string defect_keys, got: %v", err)
	}
	got := response.Results[0]
	if len(got.DefectKeys) != 2 || got.DefectKeys[0] != "原发性高血压" {
		t.Fatalf("unexpected defect keys: %v", []string(got.DefectKeys))
	}
}
