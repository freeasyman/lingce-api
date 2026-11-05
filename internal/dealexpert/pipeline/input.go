package pipeline

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
)

type RecordingInput struct {
	RecordingID    int64
	TenantID       int64
	EncounterID    *int64
	EmployeeID     int64
	ConsultantName string
	RecordedAt     *time.Time
}
type ContextInput struct {
	AnalysisAt                 time.Time
	Timezone                   string
	IdentityConfirmed          bool
	ContactAuthorized          *bool
	ContactForbidden           bool
	ClinicalHold               bool
	Coverage                   string
	Truncated                  bool
	OmittedSourceCount         *int
	CatalogComplete            bool
	AllowMissedOpportunity     bool
	AllowExtendedOpportunities bool
}
type TranscriptInput struct {
	ID          string
	Text        string
	Speaker     string
	StartMS     *int
	TimingBasis string
	RoleBasis   string
}
type BusinessFactInput struct {
	ID, Type, Text string
	SourceRecordID int64
	OccurredAt     *time.Time
}
type ProductInput struct {
	ID                           int64
	Name, Version, Description   string
	RequiresClinicalConfirmation bool
}
type InputMaterial struct {
	Recording          RecordingInput
	Context            ContextInput
	Transcript         []TranscriptInput
	Facts              []BusinessFactInput
	Products           []ProductInput
	DroppedSourceIDs   []string
	OmittedSourceCount *int
}
type InputEnvelope struct {
	InputSnapshotID    string          `json:"input_snapshot_id"`
	Input              json.RawMessage `json:"input"`
	PreprocessingAudit json.RawMessage `json:"preprocessing_audit"`
}

func (e InputEnvelope) JSON() ([]byte, error) {
	return json.Marshal(map[string]any{"input_snapshot_id": e.InputSnapshotID, "input": json.RawMessage(e.Input)})
}

// NewRunFromInput binds the explicit server snapshot to a frozen run manifest.
// It is the seam used by the future Analyzer integration; no DB or clock access
// is hidden here.
func NewRunFromInput(id string, material InputMaterial, bundle Bundle, model ModelConfig, preprocessingAudit json.RawMessage) (Run, error) {
	env, err := AssembleInput(material)
	if compiler.IsV210Rules(bundle.RulesVersion()) {
		env, err = AssembleInputV210(material, OwnershipEvidence{})
	}
	if err != nil {
		return Run{}, err
	}
	raw, err := env.JSON()
	if err != nil {
		return Run{}, err
	}
	if len(preprocessingAudit) == 0 {
		preprocessingAudit = env.PreprocessingAudit
	}
	encounter := int64(0)
	if material.Recording.EncounterID != nil {
		encounter = *material.Recording.EncounterID
	}
	return NewRun(RunSpec{ID: id, TenantID: material.Recording.TenantID, RecordingID: material.Recording.RecordingID, EncounterID: encounter, BundleID: bundle.ID(), SnapshotID: env.InputSnapshotID, Snapshot: raw, PreprocessingAudit: preprocessingAudit, Model: model}, bundle)
}

var sourceIDPattern = regexp.MustCompile(`^[sf][1-9][0-9]*$`)

// AssembleInput is deterministic and side-effect free. analysis_at must be
// supplied by the caller from its run manifest; the adapter never reads clock.
func AssembleInput(m InputMaterial) (InputEnvelope, error) {
	if m.Recording.TenantID <= 0 || m.Recording.RecordingID <= 0 || m.Recording.EmployeeID <= 0 {
		return InputEnvelope{}, fmt.Errorf("recording identity is required")
	}
	if m.Context.AnalysisAt.IsZero() || strings.TrimSpace(m.Context.Timezone) == "" {
		return InputEnvelope{}, fmt.Errorf("analysis time and timezone are required")
	}
	coverage := m.Context.Coverage
	if coverage != "complete" && coverage != "partial" && coverage != "unknown" {
		return InputEnvelope{}, fmt.Errorf("invalid coverage")
	}
	if m.Context.Truncated && coverage == "complete" {
		return InputEnvelope{}, fmt.Errorf("truncated input cannot be complete")
	}
	if m.Context.OmittedSourceCount != nil && *m.Context.OmittedSourceCount < 0 {
		return InputEnvelope{}, fmt.Errorf("negative omitted source count")
	}
	seen := map[string]bool{}
	transcript := make([]any, 0, len(m.Transcript))
	for _, s := range m.Transcript {
		if !sourceIDPattern.MatchString(s.ID) || !strings.HasPrefix(s.ID, "s") {
			return InputEnvelope{}, fmt.Errorf("invalid transcript source id %q", s.ID)
		}
		if seen[s.ID] {
			return InputEnvelope{}, fmt.Errorf("duplicate source %s", s.ID)
		}
		seen[s.ID] = true
		if strings.TrimSpace(s.Text) == "" {
			return InputEnvelope{}, fmt.Errorf("empty transcript %s must be recorded as dropped", s.ID)
		}
		if !dialogueText(s.Text) {
			return InputEnvelope{}, fmt.Errorf("transcript %s must be normalized before assembly", s.ID)
		}
		if s.Speaker != "客户" && s.Speaker != "咨询师" && s.Speaker != "家属" && s.Speaker != "医生" && s.Speaker != "未知" {
			return InputEnvelope{}, fmt.Errorf("invalid speaker %s", s.ID)
		}
		if s.TimingBasis != "asr" && s.TimingBasis != "aligned" && s.TimingBasis != "unknown" {
			return InputEnvelope{}, fmt.Errorf("invalid timing basis %s", s.ID)
		}
		if (s.StartMS == nil) != (s.TimingBasis == "unknown") {
			return InputEnvelope{}, fmt.Errorf("time basis mismatch %s", s.ID)
		}
		if s.RoleBasis != "cleaned_transcription" && s.RoleBasis != "human_verified" && s.RoleBasis != "unknown" {
			return InputEnvelope{}, fmt.Errorf("invalid role basis %s", s.ID)
		}
		if (s.Speaker == "未知") != (s.RoleBasis == "unknown") {
			return InputEnvelope{}, fmt.Errorf("role basis mismatch %s", s.ID)
		}
		transcript = append(transcript, map[string]any{"id": s.ID, "text": s.Text, "speaker": s.Speaker, "start_ms": s.StartMS, "timing_basis": s.TimingBasis, "role_basis": s.RoleBasis})
	}
	facts := make([]any, 0, len(m.Facts))
	for _, f := range m.Facts {
		if !sourceIDPattern.MatchString(f.ID) || !strings.HasPrefix(f.ID, "f") {
			return InputEnvelope{}, fmt.Errorf("invalid fact source id %q", f.ID)
		}
		if seen[f.ID] {
			return InputEnvelope{}, fmt.Errorf("duplicate source %s", f.ID)
		}
		seen[f.ID] = true
		if f.Type != "预约" && f.Type != "定金" && f.Type != "付款" && f.Type != "退款" {
			return InputEnvelope{}, fmt.Errorf("invalid fact type %s", f.ID)
		}
		if strings.TrimSpace(f.Text) == "" || f.SourceRecordID <= 0 {
			return InputEnvelope{}, fmt.Errorf("invalid fact %s", f.ID)
		}
		facts = append(facts, map[string]any{"id": f.ID, "type": f.Type, "text": f.Text, "source_record_id": f.SourceRecordID, "occurred_at": f.OccurredAt})
	}
	products := make([]any, 0, len(m.Products))
	seenProducts := map[int64]bool{}
	for _, p := range m.Products {
		if p.ID <= 0 || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Version) == "" || seenProducts[p.ID] {
			return InputEnvelope{}, fmt.Errorf("invalid or duplicate catalog product %d", p.ID)
		}
		seenProducts[p.ID] = true
		products = append(products, map[string]any{"id": p.ID, "name": p.Name, "version": p.Version, "description": nullableText(p.Description), "requires_clinical_confirmation": p.RequiresClinicalConfirmation})
	}
	omitted := m.OmittedSourceCount
	if omitted == nil {
		omitted = m.Context.OmittedSourceCount
	}
	if omitted == nil {
		zero := 0
		omitted = &zero
	}
	ctx := map[string]any{"analysis_at": m.Context.AnalysisAt.Format(time.RFC3339Nano), "timezone": m.Context.Timezone, "identity_confirmed": m.Context.IdentityConfirmed, "contact_authorized": m.Context.ContactAuthorized, "contact_forbidden": m.Context.ContactForbidden, "clinical_hold": m.Context.ClinicalHold, "coverage": coverage, "truncated": m.Context.Truncated, "omitted_source_count": omitted, "catalog_complete": m.Context.CatalogComplete, "allow_missed_opportunity": m.Context.AllowMissedOpportunity, "allow_extended_opportunities": m.Context.AllowExtendedOpportunities}
	recording := map[string]any{"recording_id": m.Recording.RecordingID, "tenant_id": m.Recording.TenantID, "encounter_id": m.Recording.EncounterID, "employee_id": m.Recording.EmployeeID, "consultant_name": m.Recording.ConsultantName, "recorded_at": formatTime(m.Recording.RecordedAt)}
	payload := map[string]any{"protocol_version": "deal-input-v2", "recording": recording, "context": ctx, "transcript_sources": transcript, "business_facts": facts, "product_catalog": products}
	raw, err := json.Marshal(payload)
	if err != nil {
		return InputEnvelope{}, err
	}
	if err := compiler.ValidateSnapshot(sealEnvelope(raw)); err != nil {
		return InputEnvelope{}, err
	}
	id, err := compiler.SnapshotID(raw)
	if err != nil {
		return InputEnvelope{}, err
	}
	audit := map[string]any{"dropped_source_ids": sortedUnique(m.DroppedSourceIDs), "omitted_source_count": omitted, "source_order": sourceOrder(transcript, facts), "truncated": m.Context.Truncated, "coverage": coverage}
	auditRaw, _ := json.Marshal(audit)
	return InputEnvelope{InputSnapshotID: id, Input: raw, PreprocessingAudit: auditRaw}, nil
}

func sealEnvelope(payload []byte) []byte {
	var v map[string]any
	_ = json.Unmarshal(payload, &v)
	id, _ := compiler.SnapshotID(payload)
	env := map[string]any{"input_snapshot_id": id, "input": v}
	raw, _ := json.Marshal(env)
	return raw
}
func nullableText(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
func formatTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339Nano)
}
func sortedUnique(v []string) []string {
	m := map[string]bool{}
	for _, s := range v {
		if s != "" {
			m[s] = true
		}
	}
	out := []string{}
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
func sourceOrder(transcript, facts []any) []string {
	out := []string{}
	for _, r := range transcript {
		out = append(out, r.(map[string]any)["id"].(string))
	}
	for _, r := range facts {
		out = append(out, r.(map[string]any)["id"].(string))
	}
	return out
}
