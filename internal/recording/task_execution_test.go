package recording

import (
	"testing"
	"time"
)

func TestValidateTaskExecution(t *testing.T) {
	now := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	noAnswer, invalid := "no_answer", "invalid"
	for _, c := range []struct {
		name  string
		req   CompleteTaskRequest
		fails bool
	}{
		{"legacy", CompleteTaskRequest{}, false},
		{"retry_requires_time", CompleteTaskRequest{ExecutionResult: &noAnswer}, true},
		{"retry_future", CompleteTaskRequest{ExecutionResult: &noAnswer, NextContactAt: &future}, false},
		{"past_time", CompleteTaskRequest{NextContactAt: &past}, true},
		{"invalid_result", CompleteTaskRequest{ExecutionResult: &invalid}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := validateTaskExecution(c.req, now); (err != nil) != c.fails {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
