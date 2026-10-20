package dealexpert

import (
	"encoding/json"
	"os"
	"testing"
)

// 官方交付包中的 8 条人工样本必须能通过我们自己的 Schema 与业务校验。
// 这是“契约回归 L1”：样本是合成的、不含患者信息，可以进仓库。
type officialSample struct {
	CaseID string `json:"case_id"`
	Input  struct {
		Recording struct {
			CleanedTranscript []struct {
				ID      string `json:"id"`
				Speaker string `json:"speaker"`
				Text    string `json:"text"`
				StartMS *int   `json:"start_ms"`
			} `json:"cleaned_transcript"`
		} `json:"recording"`
		BusinessFacts []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"business_facts"`
		AnalysisContext struct {
			ProductCatalog []struct {
				ID      int64  `json:"id"`
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"product_catalog"`
			AllowMissed   bool `json:"allow_missed_opportunity"`
			AllowExtended bool `json:"allow_extended_opportunities"`
		} `json:"analysis_context"`
	} `json:"input"`
	ExpectedOutput map[string]any `json:"expected_output"`
	HardAssertions map[string]any `json:"hard_assertions"`
}

func TestOfficialSamplesPassValidation(t *testing.T) {
	raw, err := os.ReadFile("testdata/deal-samples/E001-E008.samples.json")
	if err != nil {
		t.Fatalf("read samples: %v", err)
	}
	var samples []officialSample
	if err := json.Unmarshal(raw, &samples); err != nil {
		t.Fatalf("decode samples: %v", err)
	}
	if len(samples) != 8 {
		t.Fatalf("expected 8 samples, got %d", len(samples))
	}

	schema, _, err := loadDealCardSchema("")
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}

	for _, sample := range samples {
		ctx := cardValidationContext{
			Sources:       map[string]evidenceSource{},
			Facts:         map[string]businessFactRef{},
			Catalog:       map[int64]catalogRef{},
			AllowMissed:   sample.Input.AnalysisContext.AllowMissed,
			AllowExtended: sample.Input.AnalysisContext.AllowExtended,
		}
		for _, segment := range sample.Input.Recording.CleanedTranscript {
			ctx.Sources[segment.ID] = evidenceSource{
				ID: segment.ID, Text: segment.Text, Speaker: segment.Speaker, StartMS: segment.StartMS,
			}
		}
		for _, fact := range sample.Input.BusinessFacts {
			ctx.Sources[fact.ID] = evidenceSource{ID: fact.ID, Text: fact.Text, Speaker: "系统事实"}
			ctx.Facts[fact.ID] = businessFactRef{Type: fact.Type}
		}
		for _, product := range sample.Input.AnalysisContext.ProductCatalog {
			ctx.Catalog[product.ID] = catalogRef{Name: product.Name, Version: product.Version}
		}

		payload, err := json.Marshal(sample.ExpectedOutput)
		if err != nil {
			t.Fatalf("%s marshal: %v", sample.CaseID, err)
		}
		card, err := validateDealCard(string(payload), schema, ctx)
		if err != nil {
			t.Fatalf("%s validation failed: %v", sample.CaseID, err)
		}
		assertHardAssertions(t, sample.CaseID, card, sample.HardAssertions)
	}
}

func assertHardAssertions(t *testing.T, caseID string, card map[string]any, assertions map[string]any) {
	t.Helper()
	decision, _ := card["decision"].(map[string]any)
	conclusion, _ := card["conclusion"].(map[string]any)

	if want, ok := assertions["decision_state"].(string); ok && decision["decision_state"] != want {
		t.Fatalf("%s decision_state = %v, want %s", caseID, decision["decision_state"], want)
	}
	if want, ok := assertions["business_impact"].(string); ok && conclusion["business_impact"] != want {
		t.Fatalf("%s business_impact = %v, want %s", caseID, conclusion["business_impact"], want)
	}
	if want, ok := assertions["contact_policy"].(string); ok && card["contact_policy"] != want {
		t.Fatalf("%s contact_policy = %v, want %s", caseID, card["contact_policy"], want)
	}
	if want, exists := assertions["primary_action_type"]; exists {
		action, _ := card["primary_action"].(map[string]any)
		if want == nil {
			if action != nil {
				t.Fatalf("%s expected null action", caseID)
			}
		} else if action == nil || action["type"] != want {
			t.Fatalf("%s action type = %v, want %v", caseID, action["type"], want)
		}
	}
	if forbidden, ok := assertions["forbidden_product_types"].([]any); ok {
		if findings, ok := card["product_findings"].([]any); ok {
			for _, row := range findings {
				finding, _ := row.(map[string]any)
				for _, item := range forbidden {
					if finding["finding_type"] == item {
						t.Fatalf("%s contains forbidden finding %v", caseID, item)
					}
				}
			}
		}
	}
	if ranges, ok := assertions["quality_score_ranges"].(map[string]any); ok {
		quality, _ := card["quality"].(map[string]any)
		dimensions, _ := quality["dimensions"].(map[string]any)
		for name, raw := range ranges {
			dim, ok := dimensions[name].(map[string]any)
			if !ok {
				t.Fatalf("%s missing quality dimension %s", caseID, name)
			}
			bound, _ := raw.([]any)
			if len(bound) != 2 {
				continue
			}
			score, ok := dim["score"].(float64)
			if !ok {
				t.Fatalf("%s quality %s score is not numeric", caseID, name)
			}
			low, _ := bound[0].(float64)
			high, _ := bound[1].(float64)
			if score < low || score > high {
				t.Fatalf("%s quality %s = %v, want [%v,%v]", caseID, name, score, low, high)
			}
		}
	}
}
