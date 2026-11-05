package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

type testObject map[string]any

func fixture(t *testing.T, prefix string) (testObject, testObject) {
	t.Helper()
	read := func(name string) testObject {
		raw, err := os.ReadFile(filepath.Join("..", "compiler", "testdata", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var v testObject
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	return read(prefix + "-input"), read(prefix + "-output")
}

type fakeAudit struct {
	events         []string
	runs, compiles int
	failType       string
}

func (f *fakeAudit) AppendRun(context.Context, Run, Bundle) error {
	f.runs++
	f.events = append(f.events, "run")
	return nil
}
func (f *fakeAudit) AppendAttemptEvent(_ context.Context, e AttemptEvent) error {
	if f.failType == e.Type {
		return errors.New("audit write failed")
	}
	f.events = append(f.events, fmt.Sprintf("%s%d", e.Type, e.Attempt))
	return nil
}
func (f *fakeAudit) AppendCompilation(context.Context, string, []byte, string, string, string, []byte) error {
	f.compiles++
	f.events = append(f.events, "compile")
	return nil
}
func response(t *testing.T, content string) *llmgateway.TextInferenceResponse {
	t.Helper()
	return &llmgateway.TextInferenceResponse{RequestID: "req-1", Content: content, Usage: llmgateway.Usage{TotalTokens: 9}, ModelCode: "qwen"}
}

func TestExecutorPersistsBeforeCallAndCompiles(t *testing.T) {
	r, b := sampleRun(t)
	_, out := fixture(t, "example")
	content, _ := json.Marshal(out)
	audit := &fakeAudit{}
	called := false
	result, err := (Executor{Audit: audit}).Execute(context.Background(), r, b, func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		called = true
		if audit.events[0] != "run" || audit.events[1] != "request1" {
			t.Fatal("provider called before request audit")
		}
		raw, _ := json.Marshal(req)
		return response(t, string(content)), raw, []byte(`{"gateway":"raw"}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called || result.Status != "analyzed" || audit.compiles != 1 {
		t.Fatalf("bad result %#v events=%v", result, audit.events)
	}
	want := []string{"run", "request1", "response1", "compile"}
	if strings.Join(audit.events, ",") != strings.Join(want, ",") {
		t.Fatal(audit.events)
	}
}

func TestExecutorOneCorrectionOnly(t *testing.T) {
	r, b := sampleRun(t)
	_, valid := fixture(t, "example")
	good, _ := json.Marshal(valid)
	audit := &fakeAudit{}
	calls := 0
	result, err := (Executor{Audit: audit}).Execute(context.Background(), r, b, func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		calls++
		raw, _ := json.Marshal(req)
		if calls == 1 {
			return response(t, `{"protocol_version":"deal-analysis-v2"}`), raw, nil, nil
		}
		if len(req.Messages) != 4 || !strings.Contains(req.Messages[3].Content, "intermediate_structure") {
			t.Fatal("correction context absent")
		}
		return response(t, string(good)), raw, nil, nil
	})
	if err != nil || result.Status != "analyzed" || calls != 2 {
		t.Fatalf("result=%#v err=%v calls=%d events=%v", result, err, calls, audit.events)
	}
	want := []string{"run", "request1", "response1", "error1", "request2", "response2", "compile"}
	if strings.Join(audit.events, ",") != strings.Join(want, ",") {
		t.Fatal(audit.events)
	}
}

func TestExecutorProviderErrorIsAuditedAndNotRetried(t *testing.T) {
	r, b := sampleRun(t)
	audit := &fakeAudit{}
	calls := 0
	wantErr := errors.New("gateway down")
	_, err := (Executor{Audit: audit}).Execute(context.Background(), r, b, func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		calls++
		raw, _ := json.Marshal(req)
		return nil, raw, []byte(`{"error":"down"}`), wantErr
	})
	if !errors.Is(err, wantErr) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	want := []string{"run", "request1", "error1"}
	if strings.Join(audit.events, ",") != strings.Join(want, ",") {
		t.Fatal(audit.events)
	}
}

func TestExecutorRejectsGatewayRequestMutation(t *testing.T) {
	r, b := sampleRun(t)
	audit := &fakeAudit{}
	_, err := (Executor{Audit: audit}).Execute(context.Background(), r, b, func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		return response(t, `{}`), []byte(`{"different":true}`), nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatal(err)
	}
}

func TestExecutorDoesNotContinueWhenErrorAuditFails(t *testing.T) {
	r, b := sampleRun(t)
	audit := &fakeAudit{failType: "error"}
	calls := 0
	_, err := (Executor{Audit: audit}).Execute(context.Background(), r, b, func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		calls++
		raw, _ := json.Marshal(req)
		return nil, raw, nil, errors.New("provider")
	})
	if err == nil || !strings.Contains(err.Error(), "audit gateway error") || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestExecutorDoesNotReclassifySemanticCompilerError(t *testing.T) {
	// A structurally valid intermediate response with a wrong source is still a
	// compiler-local degradation; it must produce a card, not a model retry.
	r, b := sampleRun(t)
	audit := &fakeAudit{}
	_, out := fixture(t, "example")
	out["input_snapshot_id"] = "sha256:" + strings.Repeat("0", 64)
	bad, _ := json.Marshal(out)
	calls := 0
	_, err := (Executor{Audit: audit}).Execute(context.Background(), r, b, func(_ context.Context, req llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error) {
		calls++
		raw, _ := json.Marshal(req)
		return response(t, string(bad)), raw, nil, nil
	})
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if compiler.RulesVersion == "" {
		t.Fatal("contract missing")
	}
}
