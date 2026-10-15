package patientops

import (
	"encoding/json"
	"testing"
)

func TestScreenshotAnalysisAcceptsStringOrArrayLists(t *testing.T) {
	var analysis ScreenshotAnalysis
	if err := json.Unmarshal([]byte(`{"product_interest":"体态矫正","patient_utterances":"需要和家人商量","evidence":["询问价格"]}`), &analysis); err != nil {
		t.Fatalf("unmarshal mixed list shapes: %v", err)
	}
	if len(analysis.ProductInterest) != 1 || analysis.ProductInterest[0] != "体态矫正" {
		t.Fatalf("unexpected product_interest: %#v", analysis.ProductInterest)
	}
	if len(analysis.PatientUtterances) != 1 || analysis.PatientUtterances[0] != "需要和家人商量" {
		t.Fatalf("unexpected patient_utterances: %#v", analysis.PatientUtterances)
	}
	if len(analysis.Evidence) != 1 || analysis.Evidence[0] != "询问价格" {
		t.Fatalf("unexpected evidence: %#v", analysis.Evidence)
	}
}

func TestScreenshotAnalysisNormalizesWhitespace(t *testing.T) {
	var analysis ScreenshotAnalysis
	if err := json.Unmarshal([]byte(`{"product_interest":["  体态矫正  ",""],"patient_utterances":null}`), &analysis); err != nil {
		t.Fatal(err)
	}
	analysis.Normalize()
	if len(analysis.ProductInterest) != 1 || analysis.ProductInterest[0] != "体态矫正" {
		t.Fatalf("unexpected normalized values: %#v", analysis.ProductInterest)
	}
}
