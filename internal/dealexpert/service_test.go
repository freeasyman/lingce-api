package dealexpert

import (
	"context"
	"testing"
)

func TestReceiveRequiresVersionPair(t *testing.T) {
	s := NewService(nil)
	_, _, err := s.Receive(context.Background(), ReceiveRequest{TenantID: 1, RecordingID: 2, EncounterID: 3, EmployeeID: 4, CleanedTranscript: "客户有顾虑", AnalysisPipelineVersion: "pipeline-v1"})
	if err == nil || err.Error() != "prompt_version is required" {
		t.Fatalf("expected prompt version validation, got %v", err)
	}
}

func TestReceiveRequiresEncounter(t *testing.T) {
	s := NewService(nil)
	_, _, err := s.Receive(context.Background(), ReceiveRequest{TenantID: 1, RecordingID: 2, EmployeeID: 4, CleanedTranscript: "客户有顾虑", AnalysisPipelineVersion: "pipeline-v1", PromptVersion: "prompt-v1"})
	if err == nil || err.Error() != "tenant_id, recording_id, encounter_id and employee_id are required" {
		t.Fatalf("expected encounter validation, got %v", err)
	}
}
