package emrquality

import "testing"

func reportWithDeductions(deductions map[string]int) *Report {
	results := make([]RuleResult, 0, len(deductions))
	for section, deduction := range deductions {
		results = append(results, RuleResult{SectionCode: section, Result: "problem", Deduction: deduction})
	}
	report := &Report{Results: results, ScoreReady: true}
	calculateReport(report)
	return report
}

func TestMajorProjectDowngrade(t *testing.T) {
	cases := []struct {
		name       string
		deductions map[string]int
		wantTotal  int
		wantMajor  int
		wantGrade  string
	}{
		{
			name:       "total 89 but major 69 -> 乙级",
			deductions: map[string]int{"history": 2, "physical_exam": 2, "diagnosis": 2, "treatment": 5},
			wantTotal:  89, wantMajor: 69, wantGrade: "乙级",
		},
		{
			name:       "total 84 and major 64 -> 丙级",
			deductions: map[string]int{"history": 2, "physical_exam": 2, "diagnosis": 2, "treatment": 10},
			wantTotal:  84, wantMajor: 64, wantGrade: "丙级",
		},
		{
			name:       "total 90 and major 75 -> 甲级",
			deductions: map[string]int{"general": 5, "treatment": 5},
			wantTotal:  90, wantMajor: 75, wantGrade: "甲级",
		},
		{
			name:       "total 69 major 69 stays 丙级 (never upgrade)",
			deductions: map[string]int{"general": 5, "auxiliary_exam": 5, "signature": 5, "writing": 5, "history": 2, "physical_exam": 2, "diagnosis": 2, "treatment": 5},
			wantTotal:  69, wantMajor: 69, wantGrade: "丙级",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := reportWithDeductions(tc.deductions)
			if report.TotalScore != tc.wantTotal || report.MajorScore != tc.wantMajor {
				t.Fatalf("total/major = %d/%d, want %d/%d", report.TotalScore, report.MajorScore, tc.wantTotal, tc.wantMajor)
			}
			if report.Grade != tc.wantGrade {
				t.Fatalf("grade = %s, want %s", report.Grade, tc.wantGrade)
			}
		})
	}
}
