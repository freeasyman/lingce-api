package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/google/uuid"
)

// ModelConfig contains effective request parameters, including resolved defaults.
// It is frozen for the run and never reloaded during a correction attempt.
type ModelConfig struct {
	FunctionType   string            `json:"function_type"`
	Provider       string            `json:"provider"`
	ModelCode      string            `json:"model_code"`
	Params         llmgateway.Params `json:"params"`
	SourceConfigID int64             `json:"source_config_id"`
	SourceTenantID *int64            `json:"source_tenant_id"`
	SourceHash     string            `json:"source_hash"`
}

type RunSpec struct {
	ID          string          `json:"run_id"`
	TenantID    int64           `json:"tenant_id"`
	RecordingID int64           `json:"recording_id"`
	EncounterID int64           `json:"encounter_id"`
	BundleID    string          `json:"bundle_id"`
	SnapshotID  string          `json:"snapshot_id"`
	Model       ModelConfig     `json:"model"`
	Snapshot    json.RawMessage `json:"snapshot"`
	// Not sent to the model. Records preprocessing decisions and source inputs.
	PreprocessingAudit json.RawMessage `json:"preprocessing_audit"`
}

type Run struct {
	raw  []byte
	hash string
	spec RunSpec
}

func (r Run) ID() string    { return r.spec.ID }
func (r Run) Hash() string  { return r.hash }
func (r Run) JSON() []byte  { return bytes.Clone(r.raw) }
func (r Run) Spec() RunSpec { var s RunSpec; _ = decodeRun(r.raw, &s); return s }

func NewRun(spec RunSpec, bundle Bundle) (Run, error) {
	id, err := uuid.Parse(spec.ID)
	if err != nil || id.String() != spec.ID || id == uuid.Nil {
		return Run{}, fmt.Errorf("canonical nonzero run UUID required")
	}
	if spec.TenantID <= 0 || spec.RecordingID <= 0 || spec.EncounterID <= 0 || bundle.ID() == "" || spec.BundleID != bundle.ID() {
		return Run{}, fmt.Errorf("invalid run identity")
	}
	if bundle.spec.TenantID != 0 && bundle.spec.TenantID != spec.TenantID {
		return Run{}, fmt.Errorf("bundle tenant mismatch")
	}
	m := spec.Model
	if m.SourceConfigID <= 0 || m.SourceTenantID == nil || (*m.SourceTenantID != 0 && *m.SourceTenantID != spec.TenantID) || !hashPattern.MatchString(m.SourceHash) {
		return Run{}, fmt.Errorf("model config provenance is incomplete or outside run tenant")
	}
	p := m.Params
	if m.FunctionType != "deal_expert_analysis" || strings.TrimSpace(m.Provider) == "" || strings.TrimSpace(m.ModelCode) == "" || math.IsNaN(p.Temperature) || math.IsInf(p.Temperature, 0) || p.Temperature < 0 || p.Temperature > 2 || p.MaxTokens <= 0 || p.TimeoutSeconds <= 0 || p.ResponseFormat != "json" || p.OutputSchema != nil || p.EnableSearch != nil {
		return Run{}, fmt.Errorf("effective model config is invalid or untracked")
	}
	if err := bundle.Contract().ValidateSnapshot(spec.Snapshot); err != nil {
		return Run{}, fmt.Errorf("snapshot: %w", err)
	}
	var env struct {
		ID    string `json:"input_snapshot_id"`
		Input struct {
			Recording struct {
				TenantID    int64 `json:"tenant_id"`
				RecordingID int64 `json:"recording_id"`
				EncounterID int64 `json:"encounter_id"`
			} `json:"recording"`
		} `json:"input"`
	}
	if err := json.Unmarshal(spec.Snapshot, &env); err != nil {
		return Run{}, err
	}
	if spec.SnapshotID != env.ID || env.Input.Recording.TenantID != spec.TenantID || env.Input.Recording.RecordingID != spec.RecordingID || env.Input.Recording.EncounterID != spec.EncounterID {
		return Run{}, fmt.Errorf("snapshot/run scope mismatch")
	}
	var audit map[string]any
	if err := json.Unmarshal(spec.PreprocessingAudit, &audit); err != nil || audit == nil {
		return Run{}, fmt.Errorf("preprocessing audit object required")
	}
	// Snapshot is kept as a byte string in the manifest; json.RawMessage would
	// compact it on marshal, losing exact bytes sent to the model.
	raw, err := encodeRun(spec)
	if err != nil {
		return Run{}, err
	}
	var copySpec RunSpec
	if err := decodeRun(raw, &copySpec); err != nil {
		return Run{}, err
	}
	return Run{raw, digest(raw), copySpec}, nil
}

type runWire struct {
	RunSpec
	SnapshotBytes []byte `json:"snapshot_bytes"`
}

func encodeRun(spec RunSpec) ([]byte, error) {
	snapshot := bytes.Clone(spec.Snapshot)
	spec.Snapshot = nil
	return json.Marshal(runWire{spec, snapshot})
}
func decodeRun(raw []byte, spec *RunSpec) error {
	var wire runWire
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&wire); err != nil {
		return err
	}
	*spec = wire.RunSpec
	spec.Snapshot = wire.SnapshotBytes
	return nil
}

func ReadRun(raw []byte, bundle Bundle) (Run, error) {
	var s RunSpec
	if err := decodeRun(raw, &s); err != nil {
		return Run{}, err
	}
	r, err := NewRun(s, bundle)
	if err != nil {
		return Run{}, err
	}
	if !bytes.Equal(r.raw, raw) {
		return Run{}, fmt.Errorf("noncanonical run record")
	}
	return r, nil
}

func (r Run) Request(bundle Bundle, attempt int, priorOutput, validationError string) (llmgateway.TextInferenceRequest, []byte, error) {
	if r.hash == "" || bundle.ID() != r.spec.BundleID || attempt < 1 || attempt > 2 {
		return llmgateway.TextInferenceRequest{}, nil, fmt.Errorf("invalid request/run binding")
	}
	_, schema, _ := bundle.Contract().Contracts()
	messages := []llmgateway.Message{{Role: "system", Content: bundle.spec.SystemPrompt + "\n\nJSON Schema:\n" + string(schema)}, {Role: "user", Content: strings.ReplaceAll(bundle.spec.UserTemplate, "{{input_json}}", string(r.spec.Snapshot))}}
	if attempt == 2 {
		if priorOutput == "" || validationError == "" {
			return llmgateway.TextInferenceRequest{}, nil, fmt.Errorf("correction requires original output and structural error")
		}
		messages = append(messages, llmgateway.Message{Role: "assistant", Content: priorOutput}, llmgateway.Message{Role: "user", Content: "上次输出未通过结构校验。只纠正JSON结构/枚举/必填字段，保留输入快照绑定和判断，不创造来源。错误：" + validationError})
	}
	params := r.spec.Model.Params
	req := llmgateway.TextInferenceRequest{TenantID: r.spec.TenantID, CallerService: "lingce-api", CallerModule: "deal-expert.analysis-v2", TraceID: fmt.Sprintf("deal-v2-%s-%d", r.ID(), attempt), FunctionType: r.spec.Model.FunctionType, Provider: r.spec.Model.Provider, ModelCode: r.spec.Model.ModelCode, Messages: messages, Params: &params, Billing: &llmgateway.BillingMetadata{BusinessDomain: "deal_expert", BusinessObjectType: "recording", BusinessObjectID: r.spec.RecordingID, BillingSubject: "deal_expert_analysis", BillingScene: "encounter", CostLayer: "product", RecordingID: r.spec.RecordingID}}
	req.Billing.ModelConfigID = r.spec.Model.SourceConfigID
	raw, err := json.Marshal(req)
	return req, raw, err
}
