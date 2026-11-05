package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type auditDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type AuditStore struct{ db auditDB }

func NewAuditStore(pool *pgxpool.Pool) *AuditStore {
	if pool == nil {
		return &AuditStore{}
	}
	return &AuditStore{db: pool}
}

// NewConnectionAuditStore keeps session locks and audit writes on one acquired
// connection. The caller owns Release and must not use the connection concurrently.
func NewConnectionAuditStore(conn *pgxpool.Conn) *AuditStore {
	return &AuditStore{db: conn}
}

func NewTransactionAuditStore(tx pgx.Tx) *AuditStore {
	return &AuditStore{db: tx}
}

func hashBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func eventHash(e AttemptEvent) string {
	// Hash the exact provider request/response content as well as structured
	// metadata. A metadata-only hash cannot prove which raw model text was saved.
	data, _ := json.Marshal(struct {
		Payload                      []byte `json:"payload"`
		Raw, Gateway, Stage, Message string
	}{e.Payload, e.RawContent, e.GatewayRaw, e.ErrorStage, e.ErrorMessage})
	return hashBytes(data)
}
func jsonObject(raw []byte) ([]byte, error) {
	var v map[string]any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, fmt.Errorf("JSON object required")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON data")
		}
		return nil, err
	}
	return raw, nil
}

// PublishBundle is idempotent only for byte-identical content. It never updates
// an existing version; a same-scope/version conflict is an explicit release error.
func (s *AuditStore) PublishBundle(ctx context.Context, b Bundle) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("audit store is nil")
	}
	raw := b.JSON()
	spec := b.Spec()
	if b.ID() == "" {
		return fmt.Errorf("empty bundle")
	}
	if spec.TenantID > 0 {
		var parentScope int64
		if err := s.db.QueryRow(ctx, `SELECT tenant_id FROM deal_expert_pipeline_bundles WHERE bundle_id=$1`, spec.ParentBundleID).Scan(&parentScope); err != nil || parentScope != 0 {
			return fmt.Errorf("tenant bundle requires a published global parent")
		}
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO deal_expert_pipeline_bundles(bundle_id,tenant_id,version,prompt_version,parent_bundle_id,contract_hash,bundle,bundle_raw,bundle_hash) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7::text::jsonb,$7,$8) ON CONFLICT DO NOTHING`, b.ID(), spec.TenantID, spec.Version, spec.PromptVersion, spec.ParentBundleID, b.ContractHash(), string(raw), hashBytes(raw)); err != nil {
		return fmt.Errorf("publish bundle: %w", err)
	}
	var existing string
	err := s.db.QueryRow(ctx, `SELECT bundle_hash FROM deal_expert_pipeline_bundles WHERE tenant_id=$1 AND version=$2`, spec.TenantID, spec.Version).Scan(&existing)
	if err != nil {
		return fmt.Errorf("verify published bundle: %w", err)
	}
	if existing != hashBytes(raw) {
		return fmt.Errorf("immutable bundle version conflict")
	}
	return nil
}

// LoadBundle allows a caller to load its tenant release or a global release by
// immutable ID. The calling tenant is explicit and never inferred from scope 0.
func (s *AuditStore) LoadBundle(ctx context.Context, tenantID int64, id string) (Bundle, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return Bundle{}, fmt.Errorf("audit store and positive tenant required")
	}
	var raw, hash string
	if err := s.db.QueryRow(ctx, `SELECT bundle_raw,bundle_hash FROM deal_expert_pipeline_bundles WHERE bundle_id=$1 AND tenant_id IN (0,$2)`, id, tenantID).Scan(&raw, &hash); err != nil {
		return Bundle{}, fmt.Errorf("load bundle: %w", err)
	}
	b, err := ReadBundle([]byte(raw))
	if err != nil {
		return Bundle{}, err
	}
	if b.ID() != id || hashBytes([]byte(raw)) != hash {
		return Bundle{}, fmt.Errorf("stored bundle hash mismatch")
	}
	return b, nil
}

func (s *AuditStore) AppendRun(ctx context.Context, r Run, b Bundle) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("audit store is nil")
	}
	if r.Spec().BundleID != b.ID() {
		return fmt.Errorf("run bundle mismatch")
	}
	raw := r.JSON()
	spec := r.Spec()
	var wire struct {
		SnapshotBytes []byte `json:"snapshot_bytes"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	snapshot, err := jsonObject(wire.SnapshotBytes)
	if err != nil {
		return fmt.Errorf("run snapshot: %w", err)
	}
	manifest := raw
	model, _ := json.Marshal(spec.Model)
	audit := spec.PreprocessingAudit
	_, err = s.db.Exec(ctx, `INSERT INTO deal_expert_analysis_runs(run_id,tenant_id,recording_id,encounter_id,bundle_id,snapshot_id,snapshot,model_config,preprocessing_audit,run_manifest,run_manifest_raw,run_hash) VALUES($1,$2,$3,$4,$5,$6,$7::text::jsonb,$8::jsonb,$9::jsonb,$10::text::jsonb,$10,$11) ON CONFLICT(run_id) DO NOTHING`, spec.ID, spec.TenantID, spec.RecordingID, spec.EncounterID, spec.BundleID, spec.SnapshotID, string(snapshot), string(model), string(audit), string(manifest), r.Hash())
	if err != nil {
		return fmt.Errorf("append run: %w", err)
	}
	var existing string
	err = s.db.QueryRow(ctx, `SELECT run_hash FROM deal_expert_analysis_runs WHERE run_id=$1`, spec.ID).Scan(&existing)
	if err != nil {
		return err
	}
	if existing != r.Hash() {
		return fmt.Errorf("immutable run conflict")
	}
	return nil
}

type AttemptEvent struct {
	RunID        string
	Attempt      int
	Type         string
	Payload      []byte
	RawContent   string
	GatewayRaw   string
	ErrorStage   string
	ErrorMessage string
}

type StoredAttemptEvent struct {
	Attempt      int
	Type         string
	Payload      []byte
	RawContent   string
	GatewayRaw   string
	PayloadHash  string
	ErrorStage   string
	ErrorMessage string
}

// LoadRun returns the exact persisted manifest. The caller must supply the
// bundle selected by its immutable ID; a current prompt is never substituted.
func (s *AuditStore) LoadRun(ctx context.Context, tenantID int64, id string, bundle Bundle) (Run, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return Run{}, fmt.Errorf("audit store and positive tenant required")
	}
	var raw []byte
	var hash string
	if err := s.db.QueryRow(ctx, `SELECT run_manifest_raw,run_hash FROM deal_expert_analysis_runs WHERE run_id=$1 AND tenant_id=$2 AND bundle_id=$3`, id, tenantID, bundle.ID()).Scan(&raw, &hash); err != nil {
		return Run{}, fmt.Errorf("load run: %w", err)
	}
	r, err := ReadRun(raw, bundle)
	if err != nil {
		return Run{}, err
	}
	if r.Hash() != hash {
		return Run{}, fmt.Errorf("stored run hash mismatch")
	}
	return r, nil
}

func (s *AuditStore) ListAttemptEvents(ctx context.Context, tenantID int64, id string) ([]StoredAttemptEvent, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return nil, fmt.Errorf("audit store and positive tenant required")
	}
	rows, err := s.db.Query(ctx, `SELECT e.attempt_no,e.event_type,COALESCE(e.payload_raw,''),COALESCE(e.raw_content,''),COALESCE(e.gateway_raw,''),e.payload_hash,COALESCE(e.error_stage,''),COALESCE(e.error_message,'') FROM deal_expert_analysis_attempt_events e JOIN deal_expert_analysis_runs r ON r.run_id=e.run_id WHERE e.run_id=$1 AND r.tenant_id=$2 ORDER BY e.attempt_no,e.id`, id, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoredAttemptEvent{}
	for rows.Next() {
		var e StoredAttemptEvent
		var payload string
		if err := rows.Scan(&e.Attempt, &e.Type, &payload, &e.RawContent, &e.GatewayRaw, &e.PayloadHash, &e.ErrorStage, &e.ErrorMessage); err != nil {
			return nil, err
		}
		if payload != "" {
			e.Payload = []byte(payload)
		}
		if eventHash(AttemptEvent{Payload: e.Payload, RawContent: e.RawContent, GatewayRaw: e.GatewayRaw, ErrorStage: e.ErrorStage, ErrorMessage: e.ErrorMessage}) != e.PayloadHash {
			return nil, fmt.Errorf("stored event hash mismatch")
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type StoredCompilation struct {
	Status, CompilerVersion, ContractHash, CardHash string
	Card                                            []byte
	Diagnostics                                     []byte
}

func (s *AuditStore) LoadCompilation(ctx context.Context, tenantID int64, id string) (*StoredCompilation, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return nil, fmt.Errorf("audit store and positive tenant required")
	}
	var c StoredCompilation
	var card, diag []byte
	if err := s.db.QueryRow(ctx, `SELECT c.status,c.compiler_version,c.contract_hash,c.final_card_raw,c.diagnostics_raw,c.card_hash FROM deal_expert_analysis_compilations c JOIN deal_expert_analysis_runs r ON r.run_id=c.run_id WHERE c.run_id=$1 AND r.tenant_id=$2 ORDER BY c.id DESC LIMIT 1`, id, tenantID).Scan(&c.Status, &c.CompilerVersion, &c.ContractHash, &card, &diag, &c.CardHash); err != nil {
		return nil, err
	}
	c.Card = card
	c.Diagnostics = diag
	if hashBytes(card) != c.CardHash {
		return nil, fmt.Errorf("stored card hash mismatch")
	}
	return &c, nil
}

func (s *AuditStore) AppendAttemptEvent(ctx context.Context, e AttemptEvent) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("audit store is nil")
	}
	if e.Attempt < 1 || e.Attempt > 2 {
		return fmt.Errorf("attempt must be 1 or 2")
	}
	if e.Type != "request" && e.Type != "response" && e.Type != "error" {
		return fmt.Errorf("invalid attempt event")
	}
	if e.Type == "error" && (e.ErrorStage == "" || e.ErrorMessage == "") {
		return fmt.Errorf("error event requires stage and message")
	}
	if e.Type == "error" && (len(e.Payload) > 0 || e.RawContent != "") {
		return fmt.Errorf("error event cannot carry response payload")
	}
	if e.Type == "request" && len(e.Payload) == 0 || e.Type != "error" && (e.ErrorStage != "" || e.ErrorMessage != "") {
		return fmt.Errorf("invalid request or response event metadata")
	}
	if e.Type != "error" && len(e.Payload) == 0 && e.RawContent == "" {
		return fmt.Errorf("payload required")
	}
	var jsonPayload any
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &jsonPayload); err != nil {
			return fmt.Errorf("payload JSON: %w", err)
		}
	}
	_, err := s.db.Exec(ctx, `INSERT INTO deal_expert_analysis_attempt_events(run_id,attempt_no,event_type,payload,payload_raw,raw_content,gateway_raw,payload_hash,error_stage,error_message) VALUES($1,$2,$3,$4::text::jsonb,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,'')) ON CONFLICT(run_id,attempt_no,event_type) DO NOTHING`, e.RunID, e.Attempt, e.Type, nullableJSON(e.Payload), nullableString(e.RawContent), nullableString(e.GatewayRaw), eventHash(e), e.ErrorStage, e.ErrorMessage)
	if err != nil {
		return fmt.Errorf("append attempt event: %w", err)
	}
	var existing string
	if err := s.db.QueryRow(ctx, `SELECT payload_hash FROM deal_expert_analysis_attempt_events WHERE run_id=$1 AND attempt_no=$2 AND event_type=$3`, e.RunID, e.Attempt, e.Type).Scan(&existing); err != nil {
		return err
	}
	if existing != eventHash(e) {
		return fmt.Errorf("immutable attempt event conflict")
	}
	return nil
}
func nullableJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s *AuditStore) AppendCompilation(ctx context.Context, runID string, card []byte, status, compilerVersion, contractHash string, diagnostics []byte) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("audit store is nil")
	}
	if status != "analyzed" && status != "needs_review" {
		return fmt.Errorf("invalid compilation status")
	}
	if _, err := jsonObject(card); err != nil {
		return fmt.Errorf("card JSON: %w", err)
	}
	var diag []compiler.Diagnostic
	if err := json.Unmarshal(diagnostics, &diag); err != nil || diag == nil {
		return fmt.Errorf("diagnostics JSON array required")
	}
	h := hashBytes(card)
	_, err := s.db.Exec(ctx, `INSERT INTO deal_expert_analysis_compilations(run_id,compiler_version,contract_hash,status,final_card,final_card_raw,diagnostics,diagnostics_raw,card_hash) VALUES($1,$2,$3,$4,$5::text::jsonb,$5,$6::text::jsonb,$6,$7) ON CONFLICT(run_id,compiler_version) DO NOTHING`, runID, compilerVersion, contractHash, status, string(card), string(diagnostics), h)
	if err != nil {
		return fmt.Errorf("append compilation: %w", err)
	}
	var savedStatus, savedContract, savedCardHash, savedDiagnostics string
	if err := s.db.QueryRow(ctx, `SELECT status,contract_hash,card_hash,diagnostics_raw FROM deal_expert_analysis_compilations WHERE run_id=$1 AND compiler_version=$2`, runID, compilerVersion).Scan(&savedStatus, &savedContract, &savedCardHash, &savedDiagnostics); err != nil {
		return err
	}
	if savedStatus != status || savedContract != contractHash || savedCardHash != h || savedDiagnostics != string(diagnostics) {
		return fmt.Errorf("immutable compilation conflict")
	}
	return nil
}

func (s *AuditStore) RunExists(ctx context.Context, tenantID int64, id string) (bool, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return false, fmt.Errorf("audit store and positive tenant required")
	}
	var ok bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM deal_expert_analysis_runs WHERE run_id=$1 AND tenant_id=$2)`, id, tenantID).Scan(&ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}
