package opportunityalert

import "testing"

func TestExtractCandidateFromConversionOutcome(t *testing.T) {
	source := &RecordingAlertSource{
		AnalysisResult: JSONObject{
			"deal_outcome": map[string]any{
				"result": "意向明确未成交",
				"evidence": []any{
					map[string]any{"text": "客户认可方案，但想再考虑"},
				},
				"concern_strategy": map[string]any{
					"stuck_point_headline": "客户还有顾虑",
					"real_reason":          "价格和时间安排未定",
					"next_action":          "尽快二次跟进",
					"script":               "我们先把最担心的点讲清楚",
				},
			},
			"persuasive": map[string]any{
				"deal_status":   "意向明确未成交",
				"real_concern":  "价格未定",
				"stuck_point_headline": "客户还有顾虑",
			},
			"analysis_summary": map[string]any{
				"conversion_opportunity": "建议尽快回访，确认下一步到院时间。",
			},
		},
	}

	candidate, ok := extractCandidate(source)
	if !ok {
		t.Fatalf("expected candidate to be extracted")
	}
	if candidate.AlertType != "opportunity_not_advanced" {
		t.Fatalf("unexpected alert type: %q", candidate.AlertType)
	}
	if candidate.Title == "" || candidate.Summary == "" || candidate.Reason == "" {
		t.Fatalf("candidate should contain key text fields: %#v", candidate)
	}
	if candidate.Priority != "high" {
		t.Fatalf("expected high priority, got %q", candidate.Priority)
	}
}

func TestExtractCandidateRejectsNonOpportunityOutcome(t *testing.T) {
	source := &RecordingAlertSource{
		AnalysisResult: JSONObject{
			"deal_outcome": map[string]any{
				"result": "明确拒绝",
			},
		},
	}

	if candidate, ok := extractCandidate(source); ok || candidate != nil {
		t.Fatalf("expected no candidate, got %#v", candidate)
	}
}
