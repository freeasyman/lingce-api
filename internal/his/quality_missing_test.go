package his

import (
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/emrquality"
)

func TestMissingFieldsFromReportBirthDate(t *testing.T) {
	report := &emrquality.Report{
		RunID:         "run-1",
		BlockingCount: 1,
		Results: []emrquality.RuleResult{
			{RuleCode: "GEN-001", Result: "problem", Blocking: true,
				DefectKeys: []string{"birth_date"},
				Evidence:   []emrquality.Evidence{{Source: "record.patient_snapshot", Path: "patient.birth_date", Excerpt: "出生年月"}},
				Message:    "请补充出生日期，再完成病历。"},
		},
	}
	missing := missingFieldsFromReport(report)
	if len(missing) != 1 {
		t.Fatalf("missing = %+v, want 1 item", missing)
	}
	if missing[0].Field != "birth_date" || missing[0].Label != "出生日期" || missing[0].RuleCode != "GEN-001" {
		t.Fatalf("missing[0] = %+v", missing[0])
	}
	if got := qualityBlockedMessage(missing); got != "病历质量检查未通过：缺少出生日期" {
		t.Fatalf("message = %q", got)
	}
}

func TestMissingFieldsFromReportEvidenceFallback(t *testing.T) {
	report := &emrquality.Report{
		BlockingCount: 2,
		Results: []emrquality.RuleResult{
			{RuleCode: "GEN-005", Result: "problem", Blocking: true,
				Evidence: []emrquality.Evidence{{Source: "record.patient_snapshot", Path: "patient.drug_allergy"}}},
			{RuleCode: "CC-001", Result: "problem", Blocking: true,
				Evidence: []emrquality.Evidence{{Source: "record.content", Path: "chief_complaint"}}},
			{RuleCode: "CC-005", Result: "problem", Blocking: false},
		},
	}
	missing := missingFieldsFromReport(report)
	if len(missing) != 2 {
		t.Fatalf("missing = %+v, want 2 items", missing)
	}
	if missing[0].Field != "drug_allergy" || missing[0].Label != "药物过敏史" {
		t.Fatalf("missing[0] = %+v", missing[0])
	}
	if missing[1].Field != "chief_complaint" || missing[1].Label != "主诉" {
		t.Fatalf("missing[1] = %+v", missing[1])
	}
	if !strings.Contains(qualityBlockedMessage(missing), "药物过敏史、主诉") {
		t.Fatalf("message = %q", qualityBlockedMessage(missing))
	}
}
