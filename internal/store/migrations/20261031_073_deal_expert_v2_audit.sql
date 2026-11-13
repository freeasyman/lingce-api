-- 赢客专家 v2：不可变版本包、运行快照、逐次调用事件和编译结果。
-- Raw text is the byte-preserving replay authority; JSONB is only for queries.
-- Never rebuild hashed bytes from JSONB, which changes formatting/key order.
CREATE TABLE IF NOT EXISTS deal_expert_pipeline_bundles (
  bundle_id TEXT PRIMARY KEY,
  tenant_id BIGINT NOT NULL CHECK (tenant_id >= 0),
  version TEXT NOT NULL,
  prompt_version TEXT NOT NULL,
  parent_bundle_id TEXT REFERENCES deal_expert_pipeline_bundles(bundle_id),
  contract_hash TEXT NOT NULL CHECK (contract_hash ~ '^sha256:[a-f0-9]{64}$'),
  bundle JSONB NOT NULL,
  bundle_raw TEXT NOT NULL,
  bundle_hash TEXT NOT NULL CHECK (bundle_hash ~ '^sha256:[a-f0-9]{64}$'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (bundle = bundle_raw::jsonb),
  UNIQUE (tenant_id, version)
);

CREATE TABLE IF NOT EXISTS deal_expert_analysis_runs (
  run_id UUID PRIMARY KEY,
  tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
  recording_id BIGINT NOT NULL CHECK (recording_id > 0),
  encounter_id BIGINT NOT NULL CHECK (encounter_id > 0),
  bundle_id TEXT NOT NULL REFERENCES deal_expert_pipeline_bundles(bundle_id),
  snapshot_id TEXT NOT NULL CHECK (snapshot_id ~ '^sha256:[a-f0-9]{64}$'),
  snapshot JSONB NOT NULL,
  model_config JSONB NOT NULL,
  preprocessing_audit JSONB NOT NULL,
  run_manifest JSONB NOT NULL,
  run_manifest_raw TEXT NOT NULL,
  run_hash TEXT NOT NULL CHECK (run_hash ~ '^sha256:[a-f0-9]{64}$'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (run_manifest = run_manifest_raw::jsonb),
  UNIQUE (run_hash)
);

CREATE TABLE IF NOT EXISTS deal_expert_analysis_attempt_events (
  id BIGSERIAL PRIMARY KEY,
  run_id UUID NOT NULL REFERENCES deal_expert_analysis_runs(run_id),
  attempt_no SMALLINT NOT NULL CHECK (attempt_no IN (1,2)),
  event_type TEXT NOT NULL CHECK (event_type IN ('request','response','error')),
  payload JSONB,
  payload_raw TEXT,
  raw_content TEXT,
  gateway_raw TEXT,
  payload_hash TEXT NOT NULL CHECK (payload_hash ~ '^sha256:[a-f0-9]{64}$'),
  error_stage TEXT,
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK ((payload IS NULL AND payload_raw IS NULL) OR
         (payload IS NOT NULL AND payload_raw IS NOT NULL AND payload = payload_raw::jsonb)),
  UNIQUE (run_id, attempt_no, event_type),
  CHECK (event_type = 'request' AND payload IS NOT NULL AND error_stage IS NULL AND error_message IS NULL OR
         event_type = 'response' AND (payload IS NOT NULL OR raw_content IS NOT NULL) AND error_stage IS NULL OR
         event_type = 'error' AND error_stage IS NOT NULL AND error_message IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS deal_expert_analysis_compilations (
  id BIGSERIAL PRIMARY KEY,
  run_id UUID NOT NULL REFERENCES deal_expert_analysis_runs(run_id),
  compiler_version TEXT NOT NULL,
  contract_hash TEXT NOT NULL CHECK (contract_hash ~ '^sha256:[a-f0-9]{64}$'),
  status TEXT NOT NULL CHECK (status IN ('analyzed','needs_review')),
  final_card JSONB NOT NULL,
  final_card_raw TEXT NOT NULL,
  diagnostics JSONB NOT NULL,
  diagnostics_raw TEXT NOT NULL,
  card_hash TEXT NOT NULL CHECK (card_hash ~ '^sha256:[a-f0-9]{64}$'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (final_card = final_card_raw::jsonb AND diagnostics = diagnostics_raw::jsonb),
  CHECK (jsonb_typeof(diagnostics) = 'array'),
  UNIQUE (run_id, compiler_version)
);

-- Resolve tables and trigger function together in the selected application schema.
CREATE OR REPLACE FUNCTION deal_expert_v2_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'deal expert v2 audit rows are append-only';
END; $$;

DROP TRIGGER IF EXISTS deal_expert_pipeline_bundles_immutable ON deal_expert_pipeline_bundles;
CREATE TRIGGER deal_expert_pipeline_bundles_immutable BEFORE UPDATE OR DELETE ON deal_expert_pipeline_bundles FOR EACH ROW EXECUTE FUNCTION deal_expert_v2_immutable();
DROP TRIGGER IF EXISTS deal_expert_analysis_runs_immutable ON deal_expert_analysis_runs;
CREATE TRIGGER deal_expert_analysis_runs_immutable BEFORE UPDATE OR DELETE ON deal_expert_analysis_runs FOR EACH ROW EXECUTE FUNCTION deal_expert_v2_immutable();
DROP TRIGGER IF EXISTS deal_expert_attempt_events_immutable ON deal_expert_analysis_attempt_events;
CREATE TRIGGER deal_expert_attempt_events_immutable BEFORE UPDATE OR DELETE ON deal_expert_analysis_attempt_events FOR EACH ROW EXECUTE FUNCTION deal_expert_v2_immutable();
DROP TRIGGER IF EXISTS deal_expert_compilations_immutable ON deal_expert_analysis_compilations;
CREATE TRIGGER deal_expert_compilations_immutable BEFORE UPDATE OR DELETE ON deal_expert_analysis_compilations FOR EACH ROW EXECUTE FUNCTION deal_expert_v2_immutable();

CREATE INDEX IF NOT EXISTS idx_deal_expert_v2_runs_scope ON deal_expert_analysis_runs (tenant_id, recording_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deal_expert_v2_attempt_events_run ON deal_expert_analysis_attempt_events (run_id, attempt_no, event_type);
