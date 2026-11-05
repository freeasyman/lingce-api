package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"strings"
	"testing"
)

func TestAnalyzeIsIndependentShadowEntryPoint(t *testing.T) {
	b := base(t)
	m := material()
	env, err := AssembleInput(m)
	if err != nil {
		t.Fatal(err)
	}
	_, out := fixture(t, "example")
	out["input_snapshot_id"] = env.InputSnapshotID
	rawOut, _ := json.Marshal(out)
	audit := &fakeAudit{}
	calls := 0
	r, err := Analyze(context.Background(), AnalysisRequest{RunID: "123e4567-e89b-12d3-a456-426614174009", Material: m, Bundle: b, Model: fixtureModelConfig()}, audit, func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		calls++
		raw, _ := json.Marshal(req)
		return response(t, string(rawOut)), raw, nil, nil
	})
	if err != nil || calls != 1 || r.Card.Status != "analyzed" {
		t.Fatalf("result=%#v err=%v calls=%d", r, err, calls)
	}
	if strings.Join(audit.events, ",") != "run,request1,response1,compile" {
		t.Fatal(audit.events)
	}
}

func TestAnalyzeRejectsMissingAuditAndDoesNotCall(t *testing.T) {
	m := material()
	b := base(t)
	calls := 0
	_, err := Analyze(context.Background(), AnalysisRequest{RunID: "123e4567-e89b-12d3-a456-426614174010", Material: m, Bundle: b, Model: fixtureModelConfig()}, nil, func(context.Context, llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		calls++
		return nil, nil, nil, errors.New("should not call")
	})
	if err == nil || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
