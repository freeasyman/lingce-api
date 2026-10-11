package emrrealtime

import (
	"context"
	"github.com/freeasyman/lingce-api/internal/emrrecord"
	"testing"
)

func TestGeneratedBadgeDraftRequiresMatchingWorkerResult(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source map[string]any
		want   bool
	}{
		{"old recording", map[string]any{"recording_id": float64(1835), "llm_request_id": "old"}, false},
		{"current recording", map[string]any{"recording_id": float64(1836), "llm_request_id": "new"}, true},
		{"integer reference", map[string]any{"recording_id": int64(1836), "llm_request_id": "new"}, true},
		{"not generated", map[string]any{"recording_id": float64(1836)}, false},
		{"missing source", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := &emrrecord.Record{SourceReferences: tc.source, WorkingContent: map[string]any{"chief_complaint": "old content"}}
			if got := generatedBadgeDraft(record, 1836); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFinalizeBadgeWithoutIdentifierNeverQueriesLatestRecording(t *testing.T) {
	result, err := (&Service{}).FinalizeBadge(context.Background(), 1, 117, nil, FinalizeBadgeRequest{})
	if err != nil || result.Status != "pending" || result.EncounterID != 0 || result.RecordID != "" {
		t.Fatalf("unexpected result: %+v, %v", result, err)
	}
}
