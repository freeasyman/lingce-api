package pipeline

// All DDL and data use a transaction-private schema on the test instance.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func auditTestDB(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("DEAL_PHASE3_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set DEAL_PHASE3_DATABASE_URL to the authorized PostgreSQL test instance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = conn.Close(cleanupCtx)
	})
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	schema := "deal_v2_it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if err := tx.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error("rollback test schema:", err)
		}
		var count int
		if err := conn.QueryRow(cleanupCtx, `SELECT count(*) FROM pg_namespace WHERE nspname=$1`, schema).Scan(&count); err != nil {
			t.Error("verify schema cleanup:", err)
		} else if count != 0 {
			t.Error("test schema survived rollback")
		}
	})
	// No public fallback: IF NOT EXISTS must never select a deployed table.
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()+"; SET LOCAL search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../store/migrations/20261031_073_deal_expert_v2_audit.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(raw)); err != nil {
		t.Fatal("apply audit migration:", err)
	}
	return ctx, tx
}

func expectSQLFailure(t *testing.T, ctx context.Context, tx pgx.Tx, code, message, query string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(ctx, "SAVEPOINT expected_failure"); err != nil {
		t.Fatal(err)
	}
	_, execErr := tx.Exec(ctx, query, args...)
	_, rollbackErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT expected_failure; RELEASE SAVEPOINT expected_failure")
	if rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	var pgerr *pgconn.PgError
	if !errors.As(execErr, &pgerr) || pgerr.Code != code || !strings.Contains(pgerr.Message, message) {
		t.Fatalf("want SQLSTATE %s containing %q, got %v", code, message, execErr)
	}
}

func TestPhase3PostgresAuditMigration(t *testing.T) {
	ctx, tx := auditTestDB(t)
	_, err := tx.Exec(ctx, `
INSERT INTO deal_expert_pipeline_bundles(bundle_id,tenant_id,version,prompt_version,contract_hash,bundle,bundle_raw,bundle_hash) VALUES('b1',17,'v1','p1','sha256:'||repeat('a',64),'{}','{}','sha256:'||repeat('b',64));
INSERT INTO deal_expert_analysis_runs(run_id,tenant_id,recording_id,encounter_id,bundle_id,snapshot_id,snapshot,model_config,preprocessing_audit,run_manifest,run_manifest_raw,run_hash) VALUES('00000000-0000-0000-0000-000000000001',17,1,2,'b1','sha256:'||repeat('c',64),'{}','{}','{}','{}','{}','sha256:'||repeat('d',64));
INSERT INTO deal_expert_analysis_attempt_events(run_id,attempt_no,event_type,payload,payload_raw,raw_content,payload_hash) VALUES('00000000-0000-0000-0000-000000000001',1,'request','{}','{}','raw','sha256:'||repeat('e',64));
INSERT INTO deal_expert_analysis_compilations(run_id,compiler_version,contract_hash,status,final_card,final_card_raw,diagnostics,diagnostics_raw,card_hash) VALUES('00000000-0000-0000-0000-000000000001','r1','sha256:'||repeat('f',64),'analyzed','{}','{}','[]','[]','sha256:'||repeat('1',64));`)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"deal_expert_pipeline_bundles", "deal_expert_analysis_runs", "deal_expert_analysis_attempt_events", "deal_expert_analysis_compilations"} {
		for _, verb := range []string{"UPDATE", "DELETE"} {
			t.Run(table+"/"+verb, func(t *testing.T) {
				q := "DELETE FROM " + table
				if verb == "UPDATE" {
					q = "UPDATE " + table + " SET created_at=created_at"
				}
				expectSQLFailure(t, ctx, tx, "P0001", "append-only", q)
			})
		}
	}
	t.Run("scope_version_unique", func(t *testing.T) {
		expectSQLFailure(t, ctx, tx, "23505", "duplicate key", `INSERT INTO deal_expert_pipeline_bundles(bundle_id,tenant_id,version,prompt_version,contract_hash,bundle,bundle_raw,bundle_hash) VALUES('b2',17,'v1','p2','sha256:'||repeat('a',64),'{}','{}','sha256:'||repeat('b',64))`)
	})
	t.Run("attempt_unique", func(t *testing.T) {
		expectSQLFailure(t, ctx, tx, "23505", "duplicate key", `INSERT INTO deal_expert_analysis_attempt_events(run_id,attempt_no,event_type,payload,payload_raw,payload_hash) SELECT run_id,attempt_no,event_type,payload,payload_raw,payload_hash FROM deal_expert_analysis_attempt_events`)
	})
	t.Run("compilation_unique", func(t *testing.T) {
		expectSQLFailure(t, ctx, tx, "23505", "duplicate key", `INSERT INTO deal_expert_analysis_compilations(run_id,compiler_version,contract_hash,status,final_card,final_card_raw,diagnostics,diagnostics_raw,card_hash) SELECT run_id,compiler_version,contract_hash,status,final_card,final_card_raw,diagnostics,diagnostics_raw,card_hash FROM deal_expert_analysis_compilations`)
	})
}

func TestPhase3PostgresAuditStoreRoundTrip(t *testing.T) {
	ctx, tx := auditTestDB(t)
	s := &AuditStore{db: tx}
	r, b := sampleRun(t)
	if err := s.PublishBundle(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendRun(ctx, r, b); err != nil {
		t.Fatal(err)
	}
	t.Run("frozen_rule_selection", func(t *testing.T) {
		loaded, err := s.LoadBundle(ctx, r.Spec().TenantID, b.ID())
		if err != nil || loaded.RulesVersion() != "deal-compiler-v2.0.0" || !bytes.Equal(loaded.JSON(), b.JSON()) {
			t.Fatalf("persisted frozen contract selection: %v", err)
		}
		loadedRun, err := s.LoadRun(ctx, r.Spec().TenantID, r.ID(), loaded)
		if err != nil {
			t.Fatal(err)
		}
		_, before, err := r.Request(b, 1, "", "")
		if err != nil {
			t.Fatal(err)
		}
		_, after, err := loadedRun.Request(loaded, 1, "", "")
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("frozen request changed after DB read: %v", err)
		}
	})
	_, out := fixture(t, "example")
	output, _ := json.Marshal(out)
	result, err := compiler.Compile(r.Spec().Snapshot, output, compiler.RulesVersion)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, _ := json.Marshal(result.Diagnostics)
	_, request, err := r.Request(b, 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	event := AttemptEvent{RunID: r.ID(), Attempt: 1, Type: "request", Payload: request}
	if err := s.AppendAttemptEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	t.Run("global_bundle_run", func(t *testing.T) {
		loaded, err := s.LoadRun(ctx, r.Spec().TenantID, r.ID(), b)
		if err != nil || !bytes.Equal(loaded.JSON(), r.JSON()) {
			t.Fatalf("run round trip: %v", err)
		}
	})
	t.Run("request_exact_bytes", func(t *testing.T) {
		events, err := s.ListAttemptEvents(ctx, r.Spec().TenantID, r.ID())
		if err != nil || len(events) != 1 {
			t.Fatalf("event count: %d err=%v", len(events), err)
		}
		if !bytes.Equal(events[0].Payload, request) || events[0].PayloadHash != eventHash(event) {
			t.Fatal("persisted request bytes or hash changed")
		}
	})
	t.Run("compiler_diagnostics_array", func(t *testing.T) {
		if err := s.AppendCompilation(ctx, r.ID(), result.Card, result.Status, compiler.RulesVersion, b.ContractHash(), diagnostics); err != nil {
			t.Fatal(err)
		}
		loaded, err := s.LoadCompilation(ctx, r.Spec().TenantID, r.ID())
		if err != nil || !bytes.Equal(loaded.Card, result.Card) || loaded.CardHash != hashBytes(result.Card) {
			t.Fatalf("card round trip: %v", err)
		}
	})
}

func TestPhase3PostgresExecutorReplayAndTenantIsolation(t *testing.T) {
	ctx, tx := auditTestDB(t)
	s := &AuditStore{db: tx}
	r, b := sampleRun(t)
	if err := s.PublishBundle(ctx, b); err != nil {
		t.Fatal(err)
	}
	_, out := fixture(t, "example")
	good, _ := json.Marshal(out)
	calls := 0
	result, err := (Executor{Audit: s}).Execute(ctx, r, b, func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		calls++
		raw, _ := json.Marshal(req)
		content := `{"protocol_version":"deal-analysis-v2"}`
		if calls == 2 {
			content = string(good)
		}
		return response(t, content), raw, []byte(`{"gateway":"raw"}`), nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	tenant := r.Spec().TenantID
	t.Run("frozen_bundle_run_response_replay", func(t *testing.T) {
		loadedBundle, err := s.LoadBundle(ctx, tenant, b.ID())
		if err != nil {
			t.Fatal(err)
		}
		loadedRun, err := s.LoadRun(ctx, tenant, r.ID(), loadedBundle)
		if err != nil {
			t.Fatal(err)
		}
		events, err := s.ListAttemptEvents(ctx, tenant, r.ID())
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"request", "response", "error", "request", "response"}
		if len(events) != len(want) {
			t.Fatalf("events=%d", len(events))
		}
		for i, e := range events {
			if e.Type != want[i] {
				t.Fatalf("event %d type=%s", i, e.Type)
			}
		}
		if events[2].ErrorStage != "intermediate_structure" || events[2].ErrorMessage == "" {
			t.Fatal("structure error not preserved")
		}
		again, err := compiler.Compile(loadedRun.Spec().Snapshot, []byte(events[4].RawContent), compiler.RulesVersion)
		if err != nil || !bytes.Equal(again.Card, result.Card) {
			t.Fatalf("replay: %v", err)
		}
		stored, err := s.LoadCompilation(ctx, tenant, r.ID())
		if err != nil || !bytes.Equal(stored.Card, result.Card) {
			t.Fatalf("stored card: %v", err)
		}
	})
	t.Run("cross_tenant_no_reads", func(t *testing.T) {
		if _, err := s.LoadRun(ctx, tenant+1, r.ID(), b); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("run leak: %v", err)
		}
		events, err := s.ListAttemptEvents(ctx, tenant+1, r.ID())
		if err != nil || len(events) != 0 {
			t.Fatalf("event leak count=%d err=%v", len(events), err)
		}
		if _, err := s.LoadCompilation(ctx, tenant+1, r.ID()); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("compilation leak: %v", err)
		}
		exists, err := s.RunExists(ctx, tenant+1, r.ID())
		if err != nil || exists {
			t.Fatalf("existence leak: %v", err)
		}
		if exists, err := s.RunExists(ctx, tenant, r.ID()); err != nil || !exists {
			t.Fatalf("own run missing: %v", err)
		}
	})
	t.Run("immutable_idempotency", func(t *testing.T) {
		if err := s.PublishBundle(ctx, b); err != nil {
			t.Fatal(err)
		}
		if err := s.AppendRun(ctx, r, b); err != nil {
			t.Fatal(err)
		}
		_, request, _ := r.Request(b, 1, "", "")
		e := AttemptEvent{RunID: r.ID(), Attempt: 1, Type: "request", Payload: request}
		if err := s.AppendAttemptEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
		e.Payload = []byte(`{"changed":true}`)
		if err := s.AppendAttemptEvent(ctx, e); err == nil || !strings.Contains(err.Error(), "conflict") {
			t.Fatalf("event conflict: %v", err)
		}
		spec := r.Spec()
		spec.Model.ModelCode = "different-model"
		changed, err := NewRun(spec, b)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AppendRun(ctx, changed, b); err == nil || !strings.Contains(err.Error(), "conflict") {
			t.Fatalf("run conflict: %v", err)
		}
		bs := b.Spec()
		bs.SystemPrompt += " changed"
		different, err := NewBundle(bs)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.PublishBundle(ctx, different); err == nil || !strings.Contains(err.Error(), "conflict") {
			t.Fatalf("bundle conflict: %v", err)
		}
		diagnostics, _ := json.Marshal(result.Diagnostics)
		if err := s.AppendCompilation(ctx, r.ID(), result.Card, result.Status, compiler.RulesVersion, b.ContractHash(), diagnostics); err != nil {
			t.Fatal(err)
		}
		if err := s.AppendCompilation(ctx, r.ID(), result.Card, result.Status, compiler.RulesVersion, b.ContractHash(), []byte(`[{"path":"$","code":"changed","severity":"degraded"}]`)); err == nil || !strings.Contains(err.Error(), "conflict") {
			t.Fatalf("compilation conflict: %v", err)
		}
	})
	t.Run("tenant_bundle_parent_and_scope", func(t *testing.T) {
		tenantBundle, err := b.Fork(tenant, "tenant-release-1", "tenant-prompt-1", b.Spec().SystemPrompt, b.Spec().UserTemplate)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.PublishBundle(ctx, tenantBundle); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadBundle(ctx, tenant, tenantBundle.ID()); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadBundle(ctx, tenant+1, tenantBundle.ID()); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("tenant bundle leak: %v", err)
		}
		spec := r.Spec()
		spec.ID = "123e4567-e89b-12d3-a456-426614174099"
		spec.BundleID = tenantBundle.ID()
		tenantRun, err := NewRun(spec, tenantBundle)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AppendRun(ctx, tenantRun, tenantBundle); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadRun(ctx, tenant, tenantRun.ID(), tenantBundle); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadRun(ctx, tenant, tenantRun.ID(), b); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("wrong bundle loaded: %v", err)
		}
		missing := b.Spec()
		missing.Version = "unpublished-parent"
		parent, err := NewBundle(missing)
		if err != nil {
			t.Fatal(err)
		}
		orphan, err := parent.Fork(tenant, "orphan-1", "orphan-prompt-1", missing.SystemPrompt, missing.UserTemplate)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.PublishBundle(ctx, orphan); err == nil {
			t.Fatal("unpublished parent accepted")
		}
	})
	t.Run("gateway_failure_persisted", func(t *testing.T) {
		spec := r.Spec()
		spec.ID = "123e4567-e89b-12d3-a456-426614174098"
		failedRun, err := NewRun(spec, b)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		_, err = (Executor{Audit: s}).Execute(ctx, failedRun, b, func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
			calls++
			raw, _ := json.Marshal(req)
			return nil, raw, []byte(`{"error":"provider unavailable"}`), errors.New("provider unavailable")
		})
		if err == nil || calls != 1 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
		events, err := s.ListAttemptEvents(ctx, tenant, failedRun.ID())
		if err != nil || len(events) != 2 || events[1].ErrorStage != "gateway" || events[1].GatewayRaw == "" {
			t.Fatalf("failure events: %v", err)
		}
		if _, err := s.LoadCompilation(ctx, tenant, failedRun.ID()); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("failed run has card: %v", err)
		}
	})
}

func TestPhase3PostgresNeedsReviewAndDiagnostics(t *testing.T) {
	ctx, tx := auditTestDB(t)
	s := &AuditStore{db: tx}
	b := base(t)
	in, out := fixture(t, "insufficient")
	snapshot, _ := json.Marshal(in)
	output, _ := json.Marshal(out)
	env := in["input"].(map[string]any)
	recording := env["recording"].(map[string]any)
	spec := RunSpec{ID: "123e4567-e89b-12d3-a456-426614174097", TenantID: int64(recording["tenant_id"].(float64)), RecordingID: int64(recording["recording_id"].(float64)), EncounterID: int64(recording["encounter_id"].(float64)), BundleID: b.ID(), SnapshotID: in["input_snapshot_id"].(string), Snapshot: snapshot, PreprocessingAudit: json.RawMessage(`{"coverage":"unknown","truncated":true}`), Model: fixtureModelConfig()}
	r, err := NewRun(spec, b)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishBundle(ctx, b); err != nil {
		t.Fatal(err)
	}
	result, err := (Executor{Audit: s}).Execute(ctx, r, b, func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		raw, _ := json.Marshal(req)
		return response(t, string(output)), raw, nil, nil
	})
	if err != nil || result.Status != "needs_review" || len(result.Diagnostics) == 0 {
		t.Fatalf("status=%s diagnostics=%d err=%v", result.Status, len(result.Diagnostics), err)
	}
	stored, err := s.LoadCompilation(ctx, spec.TenantID, r.ID())
	diagnostics, _ := json.Marshal(result.Diagnostics)
	if err != nil || stored.Status != "needs_review" || !bytes.Equal(stored.Diagnostics, diagnostics) {
		t.Fatalf("stored needs_review: %v", err)
	}
	var card map[string]any
	if err := json.Unmarshal(stored.Card, &card); err != nil {
		t.Fatal(err)
	}
	if card["primary_action"] != nil {
		t.Fatal("needs_review retained action")
	}
}

func TestPhase3PostgresRuntimeConfigLoader(t *testing.T) {
	ctx, tx := auditTestDB(t)
	s := &AuditStore{db: tx}
	// Real SQL resolution using the same columns/types, isolated from live config.
	_, err := tx.Exec(ctx, `CREATE TABLE llm_model_configs(id BIGINT PRIMARY KEY,tenant_id BIGINT,function_type TEXT,provider TEXT,model_code TEXT,model_params JSON,extra_params JSON,is_active BOOLEAN,is_default BOOLEAN,deleted_at TIMESTAMPTZ,updated_at TIMESTAMPTZ);
 INSERT INTO llm_model_configs VALUES(1,0,'deal_expert_analysis','dashscope','global-model','{"temperature":0.1,"max_tokens":8192}', '{}',true,true,NULL,NOW()),
 (2,101,'deal_expert_analysis','dashscope','tenant-model','{"temperature":0}', '{"timeout_seconds":200}',true,true,NULL,NOW()),
 (3,101,'deal_expert_analysis','dashscope','inactive-model','{}','{}',false,true,NULL,NOW()),
 (4,102,'deal_expert_analysis','dashscope','other-tenant-model','{}','{}',true,true,NULL,NOW());`)
	if err != nil {
		t.Fatal(err)
	}
	b := base(t)
	if err := s.PublishBundle(ctx, b); err != nil {
		t.Fatal(err)
	}
	loaded, m, err := s.LoadRuntimeConfig(ctx, 101, b.ID())
	if err != nil || loaded.ID() != b.ID() || m.ModelCode != "tenant-model" || m.Params.Temperature != 0 || m.Params.TimeoutSeconds != 200 || m.SourceConfigID != 2 || m.SourceTenantID == nil || *m.SourceTenantID != 101 || !hashPattern.MatchString(m.SourceHash) {
		t.Fatalf("tenant resolution: %+v err=%v", m, err)
	}
	global, err := s.LoadModelConfig(ctx, 103)
	if err != nil || global.ModelCode != "global-model" || global.SourceConfigID != 1 || global.SourceTenantID == nil || *global.SourceTenantID != 0 {
		t.Fatalf("global resolution: %+v err=%v", global, err)
	}
	run, _ := sampleRun(t)
	spec := run.Spec()
	spec.Model = m
	frozen, err := NewRun(spec, b)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendRun(ctx, frozen, b); err != nil {
		t.Fatal(err)
	}
	before := frozen.JSON()
	if _, err := tx.Exec(ctx, `UPDATE llm_model_configs SET model_code='changed-model' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frozen.JSON(), before) {
		t.Fatal("frozen model changed with database row")
	}
	stored, err := s.LoadRun(ctx, spec.TenantID, frozen.ID(), b)
	if err != nil || !bytes.Equal(stored.JSON(), before) {
		t.Fatalf("frozen configuration did not survive database round trip: %v", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		req, _, err := stored.Request(b, attempt, `{"bad":true}`, "missing required field")
		if err != nil || req.ModelCode != m.ModelCode || req.Provider != m.Provider || req.Params == nil || *req.Params != m.Params || req.Billing == nil || req.Billing.ModelConfigID != m.SourceConfigID {
			t.Fatalf("attempt %d lost frozen configuration or billing identity: %v", attempt, err)
		}
	}
	again, err := s.LoadModelConfig(ctx, 101)
	if err != nil || again.ModelCode != "changed-model" || again.SourceHash == m.SourceHash {
		t.Fatalf("config change did not change audit hash: %v", err)
	}
	spec.Model = again
	changed, err := NewRun(spec, b)
	if err != nil || changed.Hash() == frozen.Hash() {
		t.Fatalf("effective model change did not change run hash: %v", err)
	}
	if _, _, err := s.LoadRuntimeConfig(ctx, 101, "sha256:"+strings.Repeat("0", 64)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing release silently fell back: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE llm_model_configs SET model_params='{"enable_search":true}' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadModelConfig(ctx, 101); err == nil {
		t.Fatal("untracked live params accepted")
	}
}

func TestPhase3PostgresSharedConfigReadOnly(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("DEAL_PHASE3_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set DEAL_PHASE3_DATABASE_URL for read-only actual config validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	s := &AuditStore{db: tx}
	m, err := s.LoadModelConfig(ctx, 17)
	if err != nil {
		t.Fatal(err)
	}
	if m.SourceConfigID <= 0 || m.SourceTenantID == nil || !hashPattern.MatchString(m.SourceHash) {
		t.Fatal("live model provenance absent")
	}
	var prompt EffectivePrompt
	err = tx.QueryRow(ctx, `SELECT version,system_prompt,user_prompt_template,output_schema::text FROM recording_analysis_prompts WHERE code='deal_expert_analysis' AND is_active=true ORDER BY id DESC LIMIT 1`).Scan(&prompt.Version, &prompt.SystemPrompt, &prompt.UserTemplate, &prompt.OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string `json:"const"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(prompt.OutputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	b, importErr := NewBundleFromEffectivePrompt(0, prompt, Bundle{})
	if schema.Properties["schema_version"].Const != "" {
		if importErr == nil {
			t.Fatal("legacy final-card prompt imported as v2 release")
		}
		t.Log("Actual legacy prompt is correctly rejected for v2 runtime; a separate immutable v2 release is required")
	} else if importErr != nil || b.ID() == "" {
		t.Fatalf("current intermediate release import: %v", importErr)
	}
	t.Logf("Read-only effective model loaded: config_id=%d scope=%d provider=%s model=%s; raw configuration hash and effective parameters frozen", m.SourceConfigID, *m.SourceTenantID, m.Provider, m.ModelCode)
}
