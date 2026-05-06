BEGIN;

CREATE TABLE IF NOT EXISTS callback_inbox (
  id BIGSERIAL PRIMARY KEY,
  vendor VARCHAR(64) NOT NULL,
  source VARCHAR(64) NOT NULL,
  event_type VARCHAR(128) NOT NULL,
  device_no VARCHAR(128),
  event_id VARCHAR(128),
  order_no VARCHAR(128),
  signature_ok BOOLEAN NOT NULL DEFAULT TRUE,
  idempotency_key VARCHAR(256) NOT NULL,
  payload_raw JSONB NOT NULL,
  processed_status VARCHAR(32) NOT NULL DEFAULT 'received',
  error_code VARCHAR(64),
  error_message TEXT,
  trace_id VARCHAR(128),
  recording_id BIGINT,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_callback_inbox_idempotency_key
ON callback_inbox (idempotency_key);

CREATE INDEX IF NOT EXISTS idx_callback_inbox_vendor_created_at
ON callback_inbox (vendor, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_callback_inbox_order_no
ON callback_inbox (order_no);

CREATE INDEX IF NOT EXISTS idx_callback_inbox_recording_id
ON callback_inbox (recording_id);

CREATE TABLE IF NOT EXISTS analysis_pipelines (
  id BIGSERIAL PRIMARY KEY,
  pipeline_code VARCHAR(128) NOT NULL,
  pipeline_version VARCHAR(64) NOT NULL,
  name VARCHAR(255) NOT NULL,
  scene_scope VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'draft',
  description TEXT,
  is_default_candidate BOOLEAN NOT NULL DEFAULT FALSE,
  created_by BIGINT,
  updated_by BIGINT,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_analysis_pipelines_code_version
ON analysis_pipelines (pipeline_code, pipeline_version);

CREATE INDEX IF NOT EXISTS idx_analysis_pipelines_status_scope
ON analysis_pipelines (status, scene_scope, updated_at DESC);

CREATE TABLE IF NOT EXISTS analysis_role_routes (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  role_id BIGINT NOT NULL,
  role_code_snapshot VARCHAR(128),
  scene_scope VARCHAR(64) NOT NULL,
  pipeline_code VARCHAR(128) NOT NULL,
  pipeline_version VARCHAR(64) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  effective_at TIMESTAMP NOT NULL,
  created_by BIGINT,
  updated_by BIGINT,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_analysis_role_routes_unique_version
ON analysis_role_routes (
  tenant_id,
  role_id,
  scene_scope,
  effective_at
);

CREATE INDEX IF NOT EXISTS idx_analysis_role_routes_lookup
ON analysis_role_routes (
  tenant_id,
  role_id,
  scene_scope,
  enabled,
  effective_at DESC
);

CREATE TABLE IF NOT EXISTS analysis_runs (
  id BIGSERIAL PRIMARY KEY,
  recording_id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  employee_id BIGINT,
  resolved_role_id BIGINT,
  resolved_role_code VARCHAR(128),
  scene_scope VARCHAR(64),
  pipeline_code VARCHAR(128) NOT NULL,
  pipeline_version VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'queued',
  trigger_source VARCHAR(64) NOT NULL,
  trace_id VARCHAR(128),
  error_code VARCHAR(64),
  error_message TEXT,
  started_at TIMESTAMP,
  ended_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_analysis_runs_recording_id
ON analysis_runs (recording_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_analysis_runs_trace_id
ON analysis_runs (trace_id);

CREATE INDEX IF NOT EXISTS idx_analysis_runs_status
ON analysis_runs (status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_analysis_runs_pipeline
ON analysis_runs (pipeline_code, pipeline_version, created_at DESC);

CREATE TABLE IF NOT EXISTS analysis_step_runs (
  id BIGSERIAL PRIMARY KEY,
  run_id BIGINT NOT NULL,
  recording_id BIGINT NOT NULL,
  step_code VARCHAR(128) NOT NULL,
  step_type VARCHAR(64) NOT NULL,
  prompt_code VARCHAR(128),
  prompt_version VARCHAR(64),
  status VARCHAR(32) NOT NULL,
  attempt INTEGER NOT NULL DEFAULT 1,
  input_digest TEXT,
  output_digest TEXT,
  tokens_used INTEGER,
  cost NUMERIC(18, 6),
  execution_time_ms INTEGER,
  error_code VARCHAR(64),
  error_message TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_analysis_step_runs_run_id
ON analysis_step_runs (run_id, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_analysis_step_runs_recording_id
ON analysis_step_runs (recording_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_analysis_step_runs_prompt_code
ON analysis_step_runs (prompt_code, created_at DESC);

COMMIT;
