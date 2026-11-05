package pipeline

import (
	"context"
	"encoding/json"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"testing"
)

func TestV210AssemblyDoesNotInferIdentityFromRole(t *testing.T) {
	m := material()
	env, err := AssembleInputV210(m, OwnershipEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	var in map[string]any
	if err = json.Unmarshal(env.Input, &in); err != nil {
		t.Fatal(err)
	}
	ctx := in["context"].(map[string]any)
	if ctx["target_binding"] != "unresolved" || ctx["identity_confirmed"] != false {
		t.Fatal(ctx)
	}
	for _, raw := range in["transcript_sources"].([]any) {
		if raw.(map[string]any)["subject_binding"] != "unresolved" {
			t.Fatal("role promoted to subject")
		}
	}
	b, err := V210Bundle()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := env.JSON()
	if err = b.Contract().ValidateSnapshot(raw); err != nil {
		t.Fatal(err)
	}
	if base(t).RulesVersion() == b.RulesVersion() {
		t.Fatal("default switched")
	}
}
func TestV210AssemblyRejectsUnknownOwnershipSource(t *testing.T) {
	_, err := AssembleInputV210(material(), OwnershipEvidence{Sources: map[string]SourceOwnership{"s999": {Subject: "target_customer"}}})
	if err == nil {
		t.Fatal("nonexistent binding accepted")
	}
}

func TestPhase3PostgresV210BundleRunRoundTrip(t *testing.T) {
	ctx, tx := auditTestDB(t)
	store := &AuditStore{db: tx}
	b, err := V210Bundle()
	if err != nil {
		t.Fatal(err)
	}
	env, err := AssembleInputV210(material(), OwnershipEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := env.JSON()
	original, _ := sampleRun(t)
	spec := original.Spec()
	spec.BundleID = b.ID()
	spec.SnapshotID = env.InputSnapshotID
	spec.Snapshot = raw
	spec.PreprocessingAudit = env.PreprocessingAudit
	// Use the material's scope, never infer identity/consent from this association.
	spec.TenantID = material().Recording.TenantID
	spec.RecordingID = material().Recording.RecordingID
	spec.EncounterID = *material().Recording.EncounterID
	run, err := NewRun(spec, b)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.PublishBundle(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err = store.AppendRun(ctx, run, b); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadBundle(ctx, spec.TenantID, b.ID())
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.LoadRun(ctx, spec.TenantID, run.ID(), loaded)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RulesVersion() != "deal-compiler-v2.1.0-rc7" || string(again.JSON()) != string(run.JSON()) {
		t.Fatal("v210 persistence drift")
	}
	if _, _, err = again.Request(loaded, 1, "", ""); err != nil {
		t.Fatal(err)
	}

	// Persist an actual v2.1 compilation, not just a request manifest.
	_, output := fixture(t, "example")
	output["protocol_version"] = "deal-analysis-v2.1"
	output["input_snapshot_id"] = env.InputSnapshotID
	decision := output["decision"].(map[string]any)
	decision["state"] = "暂缓"
	decision["target"] = "方案甲"
	decision["confidence"] = map[string]any{"level": "medium", "scope": "scheme"}
	decision["source_refs"] = []any{"s2"}
	decision["condition"] = map[string]any{"kind": "family_discussion", "summary": "先和家人商量", "source_refs": []any{"s2"}}
	decision["outcome_signals"] = []any{}
	output["additional_decisions"] = []any{}
	output["event_reports"] = []any{}
	output["coach"] = map[string]any{"observations": []any{}, "improvement": nil}
	output["proposed_action"] = nil
	modelRaw, _ := json.Marshal(output)
	result, err := loaded.Contract().Compile(raw, modelRaw)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, _ := json.Marshal(result.Diagnostics)
	if err = store.AppendCompilation(ctx, run.ID(), result.Card, result.Status, loaded.RulesVersion(), loaded.ContractHash(), diagnostics); err != nil {
		t.Fatal(err)
	}
	saved, err := store.LoadCompilation(ctx, spec.TenantID, run.ID())
	if err != nil || string(saved.Card) != string(result.Card) {
		t.Fatalf("v210 compilation persistence drift: %v", err)
	}
}

func TestV210ExecutorAllowsOneStructuralCorrection(t *testing.T) {
	b, err := V210Bundle()
	if err != nil {
		t.Fatal(err)
	}
	original, _ := sampleRun(t)
	spec := original.Spec()
	env, err := AssembleInputV210(material(), OwnershipEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := env.JSON()
	spec.BundleID = b.ID()
	spec.SnapshotID = env.InputSnapshotID
	spec.Snapshot = raw
	spec.PreprocessingAudit = env.PreprocessingAudit
	spec.TenantID = material().Recording.TenantID
	spec.RecordingID = material().Recording.RecordingID
	spec.EncounterID = *material().Recording.EncounterID
	run, err := NewRun(spec, b)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	audit := &fakeAudit{}
	_, err = (Executor{Audit: audit}).Execute(context.Background(), run, b, func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		calls++
		request, _ := json.Marshal(req)
		return response(t, `{}`), request, nil, nil
	})
	if err == nil || calls != 2 {
		t.Fatalf("want exactly one correction; calls=%d err=%v", calls, err)
	}
}
