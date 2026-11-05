package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler/v210"
)

// OwnershipEvidence is supplied by the server from independently verified records.
// It is never derived from a cleaned role label or a model response.
type OwnershipEvidence struct {
	State                  string
	TargetCustomerID       *int64
	BindingRecordID        *string
	ConsultantRoleVerified bool
	Sources                map[string]SourceOwnership
	FactCustomers          map[string]int64
}
type SourceOwnership struct {
	Subject                    string
	SpeakerID, BindingRecordID *string
}

// AssembleInputV210 reuses validated transcript preprocessing, then adds the
// independent identity layer required by v2.1. Missing evidence stays unresolved.
func AssembleInputV210(m InputMaterial, evidence OwnershipEvidence) (InputEnvelope, error) {
	base, err := AssembleInput(m)
	if err != nil {
		return InputEnvelope{}, err
	}
	var input map[string]any
	decoder := json.NewDecoder(bytes.NewReader(base.Input))
	decoder.UseNumber()
	if err = decoder.Decode(&input); err != nil {
		return InputEnvelope{}, err
	}
	input["protocol_version"] = "deal-input-v2.1"
	state := evidence.State
	if state == "" {
		state = "unresolved"
	}
	ctx := input["context"].(map[string]any)
	ctx["target_binding"] = state
	ctx["target_customer_id"] = evidence.TargetCustomerID
	ctx["binding_record_id"] = evidence.BindingRecordID
	ctx["consultant_role_verified"] = evidence.ConsultantRoleVerified
	if state != "single_customer" {
		ctx["identity_confirmed"] = false
	}
	known := map[string]bool{}
	for _, raw := range input["transcript_sources"].([]any) {
		r := raw.(map[string]any)
		id := r["id"].(string)
		known[id] = true
		b, ok := evidence.Sources[id]
		if !ok {
			b.Subject = "unresolved"
		}
		r["subject_binding"] = b.Subject
		r["speaker_id"] = b.SpeakerID
		r["binding_record_id"] = b.BindingRecordID
	}
	for id := range evidence.Sources {
		if !known[id] {
			return InputEnvelope{}, fmt.Errorf("ownership for nonexistent source %s", id)
		}
	}
	// Encounter association alone does not establish an event's target customer.
	// Unbound facts remain in the preprocessing audit rather than personal events.
	facts := []any{}
	unbound := []any{}
	for _, raw := range input["business_facts"].([]any) {
		r := raw.(map[string]any)
		id := r["id"].(string)
		customer, ok := evidence.FactCustomers[id]
		if !ok {
			unbound = append(unbound, r)
			continue
		}
		r["target_customer_id"] = customer
		facts = append(facts, r)
	}
	input["business_facts"] = facts
	payload, err := json.Marshal(input)
	if err != nil {
		return InputEnvelope{}, err
	}
	id, err := v210.SnapshotID(payload)
	if err != nil {
		return InputEnvelope{}, err
	}
	env := InputEnvelope{InputSnapshotID: id, Input: payload}
	raw, err := env.JSON()
	if err != nil {
		return InputEnvelope{}, err
	}
	if err = v210.ValidateSnapshot(raw); err != nil {
		return InputEnvelope{}, err
	}
	env.PreprocessingAudit, _ = json.Marshal(map[string]any{"source_loader_version": "deal-source-v2.1.0", "base_audit": json.RawMessage(base.PreprocessingAudit), "unbound_business_facts": unbound, "ownership_evidence": evidence})
	return env, nil
}
