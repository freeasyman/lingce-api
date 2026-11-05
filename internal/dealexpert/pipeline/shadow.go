package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ShadowRunID is stable across process restarts. A new experiment must use a
// new batch identity rather than silently calling the model again for a run.
func ShadowRunID(batch string, tenantID, analysisID int64) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("lingce/deal-v2/%s/%d/%d", batch, tenantID, analysisID))).String()
}

type ShadowResult struct {
	AnalysisID    int64           `json:"analysis_id"`
	RunID         string          `json:"run_id"`
	RecordingID   int64           `json:"recording_id"`
	Status        string          `json:"status"`
	ErrorStage    string          `json:"error_stage,omitempty"`
	Error         string          `json:"error,omitempty"`
	SnapshotID    string          `json:"snapshot_id"`
	BundleID      string          `json:"bundle_id"`
	RunHash       string          `json:"run_hash"`
	CardHash      string          `json:"card_hash,omitempty"`
	ReplayEqual   bool            `json:"replay_equal"`
	EvidenceCount int             `json:"evidence_count"`
	Attempts      int             `json:"attempts"`
	TotalTokens   int             `json:"total_tokens"`
	LatencyMS     int             `json:"latency_ms"`
	Diagnostics   json.RawMessage `json:"diagnostics,omitempty"`
}

// ExecuteShadowOnce uses a session advisory lock and refuses to call a provider
// for any run that already has an attempt. Interrupted/failed calls remain
// auditable terminal results; successful calls are recompiled from saved bytes.
func (s *AuditStore) ExecuteShadowOnce(ctx context.Context, analysisID int64, run Run, bundle Bundle, call TextCall) (ShadowResult, error) {
	spec := run.Spec()
	out := ShadowResult{AnalysisID: analysisID, RunID: run.ID(), RecordingID: spec.RecordingID, SnapshotID: spec.SnapshotID, BundleID: bundle.ID(), RunHash: run.Hash(), Status: "failed"}
	// The caller must supply a connection-bound store, never a pool, so session
	// lock acquisition and release refer to the same PostgreSQL connection.
	if s == nil || s.db == nil {
		return out, fmt.Errorf("shadow audit store required")
	}
	switch s.db.(type) {
	case *pgxpool.Conn, pgx.Tx:
	default:
		return out, fmt.Errorf("shadow execution requires an acquired pool connection")
	}
	var locked bool
	if err := s.db.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, run.ID()).Scan(&locked); err != nil {
		return out, err
	}
	if !locked {
		return out, fmt.Errorf("shadow run is already executing")
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer done()
		_, _ = s.db.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, run.ID())
	}()
	events, err := s.ListAttemptEvents(ctx, spec.TenantID, run.ID())
	if err != nil {
		return out, err
	}
	if len(events) == 0 {
		_, executeErr := (Executor{Audit: s}).Execute(ctx, run, bundle, call)
		if executeErr != nil {
			out.Error = executeErr.Error()
		}
		events, err = s.ListAttemptEvents(ctx, spec.TenantID, run.ID())
		if err != nil {
			return out, err
		}
	}
	lastContent := ""
	for _, event := range events {
		if event.Type == "request" {
			out.Attempts++
		}
		if event.Type == "error" {
			out.ErrorStage = event.ErrorStage
			out.Error = event.ErrorMessage
		}
		if event.Type == "response" {
			lastContent = event.RawContent
			var metadata struct {
				Usage struct {
					TotalTokens int `json:"total_tokens"`
				} `json:"usage"`
				LatencyMS int `json:"latency_ms"`
			}
			if json.Unmarshal(event.Payload, &metadata) != nil {
				return out, fmt.Errorf("invalid stored response metadata")
			}
			out.TotalTokens += metadata.Usage.TotalTokens
			out.LatencyMS += metadata.LatencyMS
		}
	}
	stored, err := s.LoadCompilation(ctx, spec.TenantID, run.ID())
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		if out.Error == "" {
			out.Error = "persisted request has no completed compilation; automatic recall refused"
			out.ErrorStage = "interrupted"
		}
		return out, nil
	}
	if stored.CompilerVersion != bundle.RulesVersion() || stored.ContractHash != bundle.ContractHash() {
		return out, fmt.Errorf("stored compilation version mismatch")
	}
	compiled, err := bundle.Contract().Compile(spec.Snapshot, []byte(lastContent))
	if err != nil {
		return out, fmt.Errorf("stored response replay: %w", err)
	}
	if !bytes.Equal(compiled.Card, stored.Card) {
		return out, fmt.Errorf("replayed card bytes differ")
	}
	count, err := verifySourceEvidence(bundle.Contract(), spec.Snapshot, stored.Card)
	if err != nil {
		return out, err
	}
	out.Status = stored.Status
	out.CardHash = stored.CardHash
	out.ReplayEqual = true
	out.EvidenceCount = count
	out.Diagnostics = stored.Diagnostics
	out.ErrorStage = ""
	out.Error = ""
	return out, nil
}

func verifySourceEvidence(contract compiler.Contract, snapshot, card []byte) (int, error) {
	if err := contract.ValidateCard(card); err != nil {
		return 0, err
	}
	var input struct {
		Input struct {
			Transcript []map[string]any `json:"transcript_sources"`
			Facts      []map[string]any `json:"business_facts"`
		} `json:"input"`
	}
	var final struct {
		Evidence []map[string]any `json:"evidence_refs"`
	}
	if json.Unmarshal(snapshot, &input) != nil || json.Unmarshal(card, &final) != nil {
		return 0, fmt.Errorf("invalid replay JSON")
	}
	sources := map[string]map[string]any{}
	for _, row := range input.Input.Transcript {
		sources[row["id"].(string)] = row
	}
	for _, row := range input.Input.Facts {
		sources[row["id"].(string)] = row
	}
	for _, ev := range final.Evidence {
		source := sources[fmt.Sprint(ev["source_ref"])]
		if source == nil || ev["quote"] != source["text"] {
			return 0, fmt.Errorf("evidence text does not match frozen source")
		}
		if ev["source_kind"] == "transcript" {
			for _, key := range []string{"speaker", "start_ms", "timing_basis", "role_basis"} {
				if ev[key] != source[key] {
					return 0, fmt.Errorf("evidence %s does not match source", key)
				}
			}
		}
	}
	return len(final.Evidence), nil
}
