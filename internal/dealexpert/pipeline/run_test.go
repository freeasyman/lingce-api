package pipeline

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// Synthetic DB configuration identity; real runs obtain this via LoadModelConfig.
func fixtureModelConfig() ModelConfig {
	scope := int64(0)
	return ModelConfig{FunctionType: "deal_expert_analysis", Provider: "qwen", ModelCode: "qwen3.8-max", Params: llmgateway.Params{Temperature: .1, MaxTokens: 8192, TimeoutSeconds: 180, ResponseFormat: "json"}, SourceConfigID: 1, SourceTenantID: &scope, SourceHash: hashBytes([]byte(`{"fixture":"model-config"}`))}
}

func sampleRun(t *testing.T) (Run, Bundle) {
	t.Helper()
	b := base(t)
	in, err := osRead("../compiler/testdata/example-input.json")
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(in, &env); err != nil {
		t.Fatal(err)
	}
	spec := RunSpec{ID: "123e4567-e89b-12d3-a456-426614174000", TenantID: 101, RecordingID: 1001, EncounterID: 9001, BundleID: b.ID(), SnapshotID: env["input_snapshot_id"].(string), Snapshot: in, PreprocessingAudit: json.RawMessage(`{"dropped_sources":[],"truncation":false,"source_order":"server"}`), Model: fixtureModelConfig()}
	r, err := NewRun(spec, b)
	if err != nil {
		t.Fatal(err)
	}
	return r, b
}
func osRead(path string) ([]byte, error) { return os.ReadFile(path) }

func TestRunManifestReplayAndScope(t *testing.T) {
	r, b := sampleRun(t)
	raw := r.JSON()
	again, err := ReadRun(raw, b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Hash() != again.Hash() || r.ID() != again.ID() {
		t.Fatal("run hash changed")
	}
	raw[0] = '!'
	if r.JSON()[0] != '{' {
		t.Fatal("run changed through returned bytes")
	}
	for _, tamper := range []func(*RunSpec){func(s *RunSpec) { s.TenantID = 102 }, func(s *RunSpec) { s.SnapshotID = "sha256:" + strings.Repeat("0", 64) }, func(s *RunSpec) { s.PreprocessingAudit = nil }} {
		spec := r.Spec()
		tamper(&spec)
		if _, err := NewRun(spec, b); err == nil {
			t.Fatal("tampered run accepted")
		}
	}
	modelChanged := r.Spec()
	modelChanged.Model.ModelCode = "other"
	changed, err := NewRun(modelChanged, b)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Hash() == r.Hash() {
		t.Fatal("model config mutation kept run hash")
	}
}

func TestRunRequestAndCorrection(t *testing.T) {
	r, b := sampleRun(t)
	req, raw, err := r.Request(b, 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("deal-analysis-v2")) {
		t.Fatal("request omitted intermediate contract")
	}
	if req.TraceID != "deal-v2-"+r.ID()+"-1" {
		t.Fatal("trace not run-bound")
	}
	if len(req.Messages) != 2 {
		t.Fatal("unexpected initial messages")
	}
	if _, _, err := r.Request(b, 2, "", ""); err == nil {
		t.Fatal("correction without prior output accepted")
	}
	corr, raw2, err := r.Request(b, 2, "{\"bad\":1}", "required field decision missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(corr.Messages) != 4 || !bytes.Contains(raw2, []byte("required field")) {
		t.Fatal("correction context not persisted")
	}
	for _, attempt := range []int{0, 3} {
		if _, _, err := r.Request(b, attempt, "", ""); err == nil {
			t.Fatal("invalid attempt accepted")
		}
	}
}

func TestRunRequestIsDeterministicAndDoesNotMutate(t *testing.T) {
	r, b := sampleRun(t)
	before := r.JSON()
	var wg sync.WaitGroup
	first := []byte(nil)
	var mu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, raw, err := r.Request(b, 1, "", "")
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if first == nil {
				first = raw
			} else if !bytes.Equal(first, raw) {
				t.Error("request is nondeterministic")
			}
		}()
	}
	wg.Wait()
	if !bytes.Equal(before, r.JSON()) {
		t.Fatal("request mutated run")
	}
}

func TestRunRejectsUnsafeModelParams(t *testing.T) {
	r, b := sampleRun(t)
	for _, mutate := range []func(*RunSpec){func(s *RunSpec) { s.Model.Params.ResponseFormat = "text" }, func(s *RunSpec) { s.Model.Params.OutputSchema = &llmgateway.OutputSchema{} }, func(s *RunSpec) { s.Model.Params.EnableSearch = ptr(true) }, func(s *RunSpec) { s.Model.Params.Temperature = 3 }, func(s *RunSpec) { s.Model.Params.MaxTokens = 0 }, func(s *RunSpec) { s.Model.FunctionType = "chat" }} {
		s := r.Spec()
		mutate(&s)
		if _, err := NewRun(s, b); err == nil {
			t.Fatal("unsafe model config accepted")
		}
	}
}

func TestRunRequiresFrozenModelSource(t *testing.T) {
	r, b := sampleRun(t)
	spec := r.Spec()
	scope := int64(0)
	spec.Model.SourceConfigID = 1
	spec.Model.SourceTenantID = &scope
	spec.Model.SourceHash = hashBytes([]byte(`{"fixture":"model-config"}`))
	if _, err := NewRun(spec, b); err != nil {
		t.Fatal("valid global source rejected:", err)
	}
	for name, change := range map[string]func(*ModelConfig){
		"all_absent":     func(m *ModelConfig) { m.SourceConfigID, m.SourceTenantID, m.SourceHash = 0, nil, "" },
		"id_absent":      func(m *ModelConfig) { m.SourceConfigID = 0 },
		"scope_absent":   func(m *ModelConfig) { m.SourceTenantID = nil },
		"hash_absent":    func(m *ModelConfig) { m.SourceHash = "" },
		"hash_invalid":   func(m *ModelConfig) { m.SourceHash = "invalid" },
		"scope_negative": func(m *ModelConfig) { v := int64(-1); m.SourceTenantID = &v },
		"other_tenant":   func(m *ModelConfig) { v := spec.TenantID + 1; m.SourceTenantID = &v },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := spec
			change(&candidate.Model)
			if _, err := NewRun(candidate, b); err == nil {
				t.Fatal("run accepted without complete tenant-bound model source")
			}
		})
	}
}

func TestSnapshotAndRunShareCompilerContract(t *testing.T) {
	r, b := sampleRun(t)
	if err := compiler.ValidateSnapshot(r.Spec().Snapshot); err != nil {
		t.Fatal(err)
	}
	if b.ContractHash() != ContractHash() {
		t.Fatal("contract hash mismatch")
	}
}

func TestRunFromInputUsesExplicitSnapshotAndAudit(t *testing.T) {
	b := base(t)
	m := material()
	_, err := NewRunFromInput("123e4567-e89b-12d3-a456-426614174001", m, b, fixtureModelConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m.Context.ContactAuthorized = nil
	r, err := NewRunFromInput("123e4567-e89b-12d3-a456-426614174002", m, b, fixtureModelConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(r.Spec().Snapshot, []byte(`"contact_authorized":null`)) {
		t.Fatal("run inferred contact authorization")
	}
}

func ptr(v bool) *bool { return &v }
