package emrrecord

import (
	"context"
	"testing"
)

func TestCreateAICandidateValidatesRequiredFieldsBeforeStore(t *testing.T) {
	service := &Service{}

	if _, err := service.CreateAICandidate(context.Background(), 1, "record-1", CreateAICandidateRequest{Content: map[string]any{}}); err == nil {
		t.Fatal("expected section_code validation error")
	}
	if _, err := service.CreateAICandidate(context.Background(), 1, "record-1", CreateAICandidateRequest{SectionCode: "主诉"}); err == nil {
		t.Fatal("expected content validation error")
	}
}

func TestGenerateRecordingAICandidatesRejectsEmptyCandidatesBeforeStore(t *testing.T) {
	service := &Service{}

	if _, err := service.GenerateRecordingAICandidates(context.Background(), GenerateAICandidatesRequest{
		TenantID: 1, RecordingID: 2, GenerationKey: "generation-1",
	}); err == nil {
		t.Fatal("expected empty candidates validation error")
	}
}

func TestHandleAICandidateRejectsUnknownDecisionBeforeStore(t *testing.T) {
	service := &Service{}

	if _, _, err := service.HandleAICandidate(context.Background(), 1, 2, "record-1", "candidate-1", "修改", HandleAICandidateRequest{}); err == nil {
		t.Fatal("expected invalid decision error")
	}
}

func TestDiffTopLevelCapturesAcceptedCandidateChanges(t *testing.T) {
	before := map[string]any{"主诉": "咳嗽", "现病史": "3天"}
	after := map[string]any{"主诉": "咳嗽5天", "现病史": "3天"}

	changes := diffTopLevel(before, after)
	if len(changes) != 1 {
		t.Fatalf("changes = %#v, want one change", changes)
	}
	change, ok := changes[0].(map[string]any)
	if !ok || change["field"] != "主诉" || change["before"] != "咳嗽" || change["after"] != "咳嗽5天" {
		t.Fatalf("change = %#v, want 主诉 before/after values", changes[0])
	}
}

func TestDiffTopLevelReturnsNoChangesForRejectedCandidate(t *testing.T) {
	content := map[string]any{"主诉": "咳嗽"}

	if changes := diffTopLevel(content, content); len(changes) != 0 {
		t.Fatalf("changes = %#v, want no changes", changes)
	}
}
