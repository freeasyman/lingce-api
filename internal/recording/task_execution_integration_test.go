package recording

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This test is opt-in because it exercises the real PostgreSQL transaction used
// by the follow-up execution endpoint.
func TestCompleteTaskRequeuesOnlyWhenNextContactIsSet(t *testing.T) {
	url := os.Getenv("FOLLOWUP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set FOLLOWUP_TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "recording_task_test_" + time.Now().Format("20060102150405.000000000")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA \""+schema+"\""); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA \""+schema+"\" CASCADE")
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TABLE recording_tasks (
			id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL, status TEXT,
			completed_at TIMESTAMPTZ, feedback TEXT, updated_at TIMESTAMPTZ,
			source_type TEXT, due_at TIMESTAMPTZ, execution_result TEXT,
			execution_difficulty TEXT, doctor_escalation TEXT, escalation_status TEXT,
			next_contact_at TIMESTAMPTZ, patient_replied BOOLEAN,
			revisit_attended BOOLEAN, issue_resolved BOOLEAN, patient_understanding TEXT
		);
		CREATE TABLE followup_task_audits (
			id BIGSERIAL PRIMARY KEY, tenant_id BIGINT, task_id BIGINT,
			action TEXT, detail JSONB, created_at TIMESTAMPTZ DEFAULT NOW()
		);
		INSERT INTO recording_tasks(id, tenant_id, status, source_type, due_at, updated_at)
		VALUES (1, 10, 'to_execute', 'follow_up', NOW(), NOW());
	`)
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{pool: pool}
	future := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Microsecond)
	if err := store.CompleteTask(ctx, 1, 99, stringPtr("未接通"), CompleteTaskRequest{ExecutionResult: stringPtr("no_answer"), NextContactAt: &future}); err != nil {
		t.Fatal(err)
	}
	var status string
	var completedAt *time.Time
	var dueAt time.Time
	if err := pool.QueryRow(ctx, `SELECT status, completed_at, due_at FROM recording_tasks WHERE id=1`).Scan(&status, &completedAt, &dueAt); err != nil {
		t.Fatal(err)
	}
	if status != "to_execute" || completedAt != nil || !dueAt.Equal(future) {
		t.Fatalf("requeue result: status=%q completed_at=%v due_at=%v want to_execute, NULL, %v", status, completedAt, dueAt, future)
	}
	var detail []byte
	if err := pool.QueryRow(ctx, `SELECT detail FROM followup_task_audits WHERE task_id=1 AND action='execution_recorded' ORDER BY id DESC LIMIT 1`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	var audit map[string]any
	if err := json.Unmarshal(detail, &audit); err != nil {
		t.Fatal(err)
	}
	if audit["requeued"] != true {
		t.Fatalf("audit requeued=%v, want true", audit["requeued"])
	}

	if _, err := pool.Exec(ctx, `UPDATE recording_tasks SET status='to_execute', completed_at=NULL, due_at=NOW(), updated_at=NOW()`); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteTask(ctx, 1, 99, stringPtr("多次未接通，结束任务"), CompleteTaskRequest{ExecutionResult: stringPtr("no_answer")}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status, completed_at FROM recording_tasks WHERE id=1`).Scan(&status, &completedAt); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || completedAt == nil {
		t.Fatalf("finish result: status=%q completed_at=%v want completed and non-NULL", status, completedAt)
	}
	if err := pool.QueryRow(ctx, `SELECT detail FROM followup_task_audits WHERE task_id=1 AND action='execution_recorded' ORDER BY id DESC LIMIT 1`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(detail, &audit); err != nil {
		t.Fatal(err)
	}
	if audit["requeued"] != false {
		t.Fatalf("audit requeued=%v, want false", audit["requeued"])
	}
}
