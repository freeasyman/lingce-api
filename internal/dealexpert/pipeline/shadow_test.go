package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

func TestShadowIdentityStableAndDistinct(t *testing.T) {
	a := ShadowRunID("batch1", 17, 12)
	if a != ShadowRunID("batch1", 17, 12) || a == ShadowRunID("batch2", 17, 12) || a == ShadowRunID("batch1", 18, 12) || a == ShadowRunID("batch1", 17, 13) {
		t.Fatal("shadow identity does not bind experiment and sample")
	}
}

func TestPhase3PostgresShadowExactlyOnceAndReplay(t *testing.T) {
	ctx, tx := auditTestDB(t)
	store := NewTransactionAuditStore(tx)
	r, b := sampleRun(t)
	if err := store.PublishBundle(ctx, b); err != nil {
		t.Fatal(err)
	}
	_, fixture := fixture(t, "example")
	content, _ := json.Marshal(fixture)
	calls := 0
	call := func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		calls++
		raw, _ := json.Marshal(req)
		return response(t, string(content)), raw, nil, nil
	}
	first, err := store.ExecuteShadowOnce(ctx, 12, r, b, call)
	if err != nil || !first.ReplayEqual || first.EvidenceCount == 0 || calls != 1 {
		t.Fatalf("first=%+v err=%v calls=%d", first, err, calls)
	}
	again, err := store.ExecuteShadowOnce(ctx, 12, r, b, call)
	if err != nil || !again.ReplayEqual || first.CardHash != again.CardHash || calls != 1 {
		t.Fatalf("again=%+v err=%v calls=%d", again, err, calls)
	}
	spec := r.Spec()
	spec.ID = ShadowRunID("interrupted", spec.TenantID, 12)
	interrupted, err := NewRun(spec, b)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.AppendRun(ctx, interrupted, b); err != nil {
		t.Fatal(err)
	}
	_, req, _ := interrupted.Request(b, 1, "", "")
	if err = store.AppendAttemptEvent(ctx, AttemptEvent{RunID: interrupted.ID(), Attempt: 1, Type: "request", Payload: req}); err != nil {
		t.Fatal(err)
	}
	stopped, err := store.ExecuteShadowOnce(ctx, 12, interrupted, b, call)
	if err != nil || stopped.Status != "failed" || stopped.ErrorStage != "interrupted" || calls != 1 {
		t.Fatalf("interrupted=%+v err=%v calls=%d", stopped, err, calls)
	}
}
