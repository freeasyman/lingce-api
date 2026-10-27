package followup

// PostgreSQL 集成测试只使用临时 schema，不调用模型、不接触业务数据。
// 设置 FOLLOWUP_TEST_DATABASE_URL 后运行；覆盖真实 JOIN、行锁与并发接收。
// 署名：Codex；时间：2026-10-06。
import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testService(t *testing.T) *Service {
	t.Helper()
	url := os.Getenv("FOLLOWUP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set FOLLOWUP_TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("followup_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 12
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
	CREATE TABLE tenants (id bigint PRIMARY KEY, deleted_at timestamp);
	CREATE TABLE employees (id bigint PRIMARY KEY, tenant_id bigint, deleted_at timestamp);
	CREATE TABLE customers (id bigint PRIMARY KEY, tenant_id bigint, patient_id bigint, deleted_at timestamp);
	CREATE TABLE institution_employee_roles (id bigint PRIMARY KEY, tenant_id bigint, employee_id bigint, role_code text, updated_at timestamp, created_at timestamp);
	CREATE TABLE encounters (id bigint PRIMARY KEY, tenant_id bigint, source_type text, source_id bigint, provider_id bigint, patient_id bigint);
	CREATE TABLE recordings (id bigint PRIMARY KEY, tenant_id bigint, encounter_id bigint, employee_id bigint, customer_id bigint, patient_id bigint, business_scope text, resolved_role_code text);
	CREATE TABLE followup_generation_runs (
	 id bigserial PRIMARY KEY, tenant_id bigint, recording_id bigint, encounter_id bigint,
	 employee_id bigint, customer_id bigint, customer_name text, doctor_name text, recorded_at timestamp,
	 request_id text NOT NULL UNIQUE, request_payload jsonb, analysis_run_id text, prompt_code text, status text,
	 attempt_count integer, max_attempts integer, created_at timestamp, updated_at timestamp
	);
	INSERT INTO tenants VALUES (1,NULL),(2,NULL);
	INSERT INTO employees VALUES (10,1,NULL),(20,2,NULL);
	INSERT INTO customers VALUES (30,1,40,NULL),(31,1,41,NULL),(32,2,42,NULL);
	INSERT INTO institution_employee_roles VALUES (1,1,10,'doctor',NOW(),NOW());
	INSERT INTO encounters VALUES (50,1,'realtime',123,10,40),(51,1,'recording',61,10,40);
	INSERT INTO recordings VALUES (60,1,50,10,30,40,'doctor','doctor'),(61,1,51,10,30,40,'doctor','doctor');
	`)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{pool: pool}
}

func testRequest() GenerateRequest {
	return GenerateRequest{TenantID: 1, RecordingID: 60, EncounterID: 50, EmployeeID: 10, CustomerID: 30,
		DoctorName: "医生", CustomerName: "患者", RecordedAt: "2026-10-06T10:00:00+08:00", CleanedTranscript: "医生交代三天后复诊。"}
}

func TestValidateReferencesSources(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	req := testRequest()
	if err := s.validateReferences(ctx, req); err != nil {
		t.Fatal(err)
	}
	req.RecordingID, req.EncounterID = 61, 51
	if err := s.validateReferences(ctx, req); err != nil {
		t.Fatal(err)
	}
	var source string
	if err := s.pool.QueryRow(ctx, `SELECT source_type FROM encounters WHERE id=50`).Scan(&source); err != nil || source != "realtime" {
		t.Fatalf("source changed: %s, %v", source, err)
	}
}

func TestValidateReferencesRejectsMismatch(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*GenerateRequest)
		sql    string
	}{
		{name: "tenant", mutate: func(r *GenerateRequest) { r.TenantID = 2 }},
		{name: "recording", mutate: func(r *GenerateRequest) { r.RecordingID = 999 }},
		{name: "encounter", mutate: func(r *GenerateRequest) { r.EncounterID = 51 }},
		{name: "employee", mutate: func(r *GenerateRequest) { r.EmployeeID = 20 }},
		{name: "customer", mutate: func(r *GenerateRequest) { r.CustomerID = 31 }},
		{name: "provider", sql: `UPDATE encounters SET provider_id=20 WHERE id=50`},
		{name: "encounter_patient", sql: `UPDATE encounters SET patient_id=41 WHERE id=50`},
		{name: "customer_patient", sql: `UPDATE customers SET patient_id=41 WHERE id=30`},
		{name: "role", sql: `UPDATE institution_employee_roles SET role_code='consultant'`},
		{name: "deleted_employee", sql: `UPDATE employees SET deleted_at=NOW() WHERE id=10`},
		{name: "foreign_customer", sql: `UPDATE customers SET tenant_id=2 WHERE id=30`},
		{name: "foreign_encounter", sql: `UPDATE encounters SET tenant_id=2 WHERE id=50`},
		{name: "recording_source", sql: `UPDATE encounters SET source_type='recording',source_id=999 WHERE id=50`},
		{name: "recording_non_doctor", mutate: func(r *GenerateRequest) { r.RecordingID = 61; r.EncounterID = 51 }, sql: `UPDATE recordings SET business_scope='consultant',resolved_role_code='consultant' WHERE id=61`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 每个用例结束后恢复测试数据，互不影响。
			if tc.sql != "" {
				if _, err := s.pool.Exec(ctx, tc.sql); err != nil {
					t.Fatal(err)
				}
			}
			req := testRequest()
			if tc.mutate != nil {
				tc.mutate(&req)
			}
			err := s.validateReferences(ctx, req)
			var ref *InvalidReferenceError
			if !errors.As(err, &ref) {
				t.Fatalf("want InvalidReferenceError, got %v", err)
			}
			_, err = s.pool.Exec(ctx, `UPDATE encounters SET tenant_id=1,source_type='realtime',source_id=123,provider_id=10,patient_id=40 WHERE id=50; UPDATE customers SET tenant_id=1,patient_id=40 WHERE id=30; UPDATE employees SET deleted_at=NULL WHERE id=10; UPDATE institution_employee_roles SET role_code='doctor'; UPDATE recordings SET business_scope='doctor',resolved_role_code='doctor' WHERE id IN (60,61);`)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConcurrentAcceptanceReturnsOriginalRequest(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	const n = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	type result struct {
		id      int64
		request string
		created bool
		err     error
	}
	results := make(chan result, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			id, request, created, err := s.acceptGenerationRun(ctx, fmt.Sprintf("request-%d", i), testRequest())
			results <- result{id, request, created, err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	var original result
	createdCount := 0
	all := []result{}
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		all = append(all, r)
		if r.created {
			original = r
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("created=%d, want 1", createdCount)
	}
	for _, r := range all {
		if r.id != original.id || r.request != original.request {
			t.Fatalf("different receipt: %+v vs %+v", r, original)
		}
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM followup_generation_runs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d, err=%v", count, err)
	}
	// A completed or failed generation must not be restarted by redelivery.
	for _, status := range []string{"succeeded", "failed", "processing", "retry_wait"} {
		if _, err := s.pool.Exec(ctx, `UPDATE followup_generation_runs SET status=$1`, status); err != nil {
			t.Fatal(err)
		}
		resp, err := s.AcceptRequest(ctx, testRequest())
		if err != nil {
			t.Fatal(err)
		}
		if resp.RequestID != original.request || resp.Status != "accepted" {
			t.Fatalf("unexpected response: %+v", resp)
		}
		var actual string
		if err := s.pool.QueryRow(ctx, `SELECT status FROM followup_generation_runs`).Scan(&actual); err != nil || actual != status {
			t.Fatalf("status changed: %s, %v", actual, err)
		}
	}
}

// TestAcceptGenerationRunUsesAnalysisRunID 验证幂等键从录音改为分析 run：
// 同一 run 的重复回调只接收一次，新的 run（重新分析）必须创建新的生成记录。
func TestAcceptGenerationRunUsesAnalysisRunID(t *testing.T) {
	s := testService(t)
	ctx := context.Background()

	first := testRequest()
	first.AnalysisRunID = "run-1"
	id1, request1, created1, err := s.acceptGenerationRun(ctx, "request-1", first)
	if err != nil || !created1 {
		t.Fatalf("first run: id=%d request=%s created=%v err=%v", id1, request1, created1, err)
	}

	duplicate := testRequest()
	duplicate.AnalysisRunID = "run-1"
	idDup, requestDup, createdDup, err := s.acceptGenerationRun(ctx, "request-duplicate", duplicate)
	if err != nil || createdDup {
		t.Fatalf("duplicate run: id=%d request=%s created=%v err=%v", idDup, requestDup, createdDup, err)
	}
	if idDup != id1 || requestDup != request1 {
		t.Fatalf("duplicate returned different receipt: %d/%s vs %d/%s", idDup, requestDup, id1, request1)
	}

	second := testRequest()
	second.AnalysisRunID = "run-2"
	id2, request2, created2, err := s.acceptGenerationRun(ctx, "request-2", second)
	if err != nil || !created2 {
		t.Fatalf("second run: id=%d request=%s created=%v err=%v", id2, request2, created2, err)
	}
	if id2 == id1 || request2 == request1 {
		t.Fatalf("new analysis run reused old receipt: %d/%s vs %d/%s", id2, request2, id1, request1)
	}

	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM followup_generation_runs WHERE analysis_run_id IS NOT NULL`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("analysis run count=%d, err=%v", count, err)
	}
}
