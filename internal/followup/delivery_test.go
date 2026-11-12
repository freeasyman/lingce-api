package followup

// settleGeneratedTasks 的 PostgreSQL 集成测试：验证空交付落库为 failed
// （EMPTY_DELIVERY）且不扣量，非空交付落库为 succeeded 并只扣一次。
// 设置 FOLLOWUP_TEST_DATABASE_URL 后运行；不调用模型、不接触业务数据。
//
// 署名：Codex
// 时间：2026-11-12

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

func TestSettleGeneratedTasksPersistsDeliveryStatus(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `
		ALTER TABLE followup_generation_runs
			ADD COLUMN prompt_version text,
			ADD COLUMN generation_model text,
			ADD COLUMN llm_request_id text,
			ADD COLUMN llm_response_payload jsonb,
			ADD COLUMN generated_tasks_payload jsonb,
			ADD COLUMN persisted_task_count integer,
			ADD COLUMN next_retry_at timestamp,
			ADD COLUMN last_error_code text,
			ADD COLUMN last_error_message text,
			ADD COLUMN failed_at timestamp,
			ADD COLUMN completed_at timestamp;
	`)
	if err != nil {
		t.Fatal(err)
	}

	req := testRequest()
	requestPayload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	insertRun := func(t *testing.T) int64 {
		t.Helper()
		var runID int64
		if err := s.pool.QueryRow(ctx, `
			INSERT INTO followup_generation_runs (
				tenant_id, recording_id, encounter_id, employee_id, customer_id,
				request_id, request_payload, status, attempt_count, max_attempts, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,'processing',0,3,NOW(),NOW())
			RETURNING id
		`, req.TenantID, req.RecordingID, req.EncounterID, req.EmployeeID, req.CustomerID,
			fmt.Sprintf("delivery-test-%d", time.Now().UnixNano()), requestPayload).Scan(&runID); err != nil {
			t.Fatal(err)
		}
		return runID
	}

	fake := newFakeFollowupBilling()
	s.billing = fake
	s.lookupRecordingDuration = func(context.Context, int64, int64) (int64, error) {
		return 5400, nil
	}
	response := &llmgateway.TextInferenceResponse{RequestID: "delivery-test"}

	t.Run("empty delivery is failed and not charged", func(t *testing.T) {
		run := &followupGenerationRun{ID: insertRun(t), Request: req, AttemptCount: 0, MaxAttempts: 3}
		s.settleGeneratedTasks(ctx, run, "v1", "test-model", response, []byte(`{"tasks":[]}`), 0)

		var status, code, message string
		var persisted int
		if err := s.pool.QueryRow(ctx, `
			SELECT status, COALESCE(last_error_code, ''), COALESCE(last_error_message, ''),
			       COALESCE(persisted_task_count, 0)
			FROM followup_generation_runs WHERE id=$1
		`, run.ID).Scan(&status, &code, &message, &persisted); err != nil {
			t.Fatal(err)
		}
		if status != followupRunStatusFailed {
			t.Fatalf("status = %s, want failed", status)
		}
		if code != followupEmptyDeliveryCode {
			t.Fatalf("last_error_code = %s, want %s", code, followupEmptyDeliveryCode)
		}
		if !strings.Contains(message, "模型未生成任何随访任务") {
			t.Fatalf("last_error_message = %q, want empty-delivery explanation", message)
		}
		if persisted != 0 {
			t.Fatalf("persisted_task_count = %d, want 0", persisted)
		}
		if got := fake.deductCount(); got != 0 {
			t.Fatalf("empty delivery deduct calls = %d, want 0", got)
		}
	})

	t.Run("non-empty delivery is succeeded and charged once", func(t *testing.T) {
		run := &followupGenerationRun{ID: insertRun(t), Request: req, AttemptCount: 0, MaxAttempts: 3}
		s.settleGeneratedTasks(ctx, run, "v1", "test-model", response, []byte(`{"tasks":[{"title":"复查提醒"}]}`), 1)

		var status string
		var code, message *string
		var persisted int
		if err := s.pool.QueryRow(ctx, `
			SELECT status, last_error_code, last_error_message, COALESCE(persisted_task_count, 0)
			FROM followup_generation_runs WHERE id=$1
		`, run.ID).Scan(&status, &code, &message, &persisted); err != nil {
			t.Fatal(err)
		}
		if status != followupRunStatusSucceeded {
			t.Fatalf("status = %s, want succeeded", status)
		}
		if code != nil || message != nil {
			t.Fatalf("unexpected error fields: code=%v message=%v", code, message)
		}
		if persisted != 1 {
			t.Fatalf("persisted_task_count = %d, want 1", persisted)
		}
		if got := fake.deductCount(); got != 1 {
			t.Fatalf("non-empty delivery deduct calls = %d, want 1", got)
		}
		if got := fake.chargedCount("followup:recording:60:analysis"); got != 1 {
			t.Fatalf("effective deductions = %d, want 1", got)
		}
	})
}
