CREATE TABLE IF NOT EXISTS emr_rule_runs (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  record_id BIGINT NOT NULL,
  encounter_id BIGINT NULL,
  patient_id BIGINT NULL,
  template_code VARCHAR(64) NOT NULL DEFAULT '',
  template_name TEXT NOT NULL DEFAULT '',
  stage VARCHAR(32) NOT NULL,
  trigger_source VARCHAR(32) NOT NULL,
  trigger_user_id BIGINT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'running',
  error_message TEXT NOT NULL DEFAULT '',
  input_record_version INT NULL,
  input_snapshot_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_rule_runs_stage_check CHECK (stage IN ('realtime', 'save', 'pre_submit', 'pre_archive', 'post_archive_qc', 'template_publish')),
  CONSTRAINT emr_rule_runs_trigger_source_check CHECK (trigger_source IN ('user_input', 'save', 'submit', 'archive', 'batch_qc', 'system_timer', 'manual')),
  CONSTRAINT emr_rule_runs_status_check CHECK (status IN ('running', 'success', 'partial_success', 'failed', 'timeout', 'cancelled'))
);

CREATE INDEX IF NOT EXISTS idx_emr_rule_runs_tenant_record
  ON emr_rule_runs (tenant_id, record_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_emr_rule_runs_stage
  ON emr_rule_runs (tenant_id, stage, created_at DESC);

CREATE TABLE IF NOT EXISTS emr_rule_hits (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  run_id BIGINT NOT NULL REFERENCES emr_rule_runs(id) ON DELETE CASCADE,
  record_id BIGINT NOT NULL,
  rule_id TEXT NOT NULL,
  rule_code VARCHAR(128) NOT NULL,
  rule_version VARCHAR(64) NOT NULL DEFAULT 'v1',
  rule_name_snapshot TEXT NOT NULL DEFAULT '',
  stage VARCHAR(32) NOT NULL,
  executor_type VARCHAR(32) NOT NULL,
  target_type VARCHAR(32) NOT NULL,
  target_path TEXT NOT NULL DEFAULT '',
  hit_status VARCHAR(32) NOT NULL,
  severity VARCHAR(32) NOT NULL,
  action_policy_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  doctor_message TEXT NOT NULL DEFAULT '',
  qc_message TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  current_state VARCHAR(32) NOT NULL DEFAULT 'open',
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  superseded_by_hit_id BIGINT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_rule_hits_stage_check CHECK (stage IN ('realtime', 'save', 'pre_submit', 'pre_archive', 'post_archive_qc', 'template_publish')),
  CONSTRAINT emr_rule_hits_executor_type_check CHECK (executor_type IN ('hard', 'structured', 'llm', 'manual')),
  CONSTRAINT emr_rule_hits_hit_status_check CHECK (hit_status IN ('not_hit', 'hit', 'skipped', 'failed', 'timeout')),
  CONSTRAINT emr_rule_hits_severity_check CHECK (severity IN ('blocking', 'important', 'notice')),
  CONSTRAINT emr_rule_hits_current_state_check CHECK (current_state IN ('open', 'located', 'edited', 'resolved', 'not_applicable', 'acknowledged', 'qc_approved', 'qc_rejected'))
);

CREATE INDEX IF NOT EXISTS idx_emr_rule_hits_tenant_record_active
  ON emr_rule_hits (tenant_id, record_id, is_active, current_state);

CREATE INDEX IF NOT EXISTS idx_emr_rule_hits_run_id
  ON emr_rule_hits (run_id);

CREATE INDEX IF NOT EXISTS idx_emr_rule_hits_rule_code
  ON emr_rule_hits (tenant_id, rule_code, created_at DESC);

CREATE TABLE IF NOT EXISTS emr_rule_evidences (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  hit_id BIGINT NOT NULL REFERENCES emr_rule_hits(id) ON DELETE CASCADE,
  evidence_type VARCHAR(32) NOT NULL,
  field_path TEXT NOT NULL DEFAULT '',
  field_label TEXT NOT NULL DEFAULT '',
  text_excerpt TEXT NOT NULL DEFAULT '',
  object_type VARCHAR(32) NOT NULL DEFAULT '',
  object_id TEXT NOT NULL DEFAULT '',
  structured_value_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  expected_value_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  actual_value_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_rule_evidences_type_check CHECK (evidence_type IN ('field', 'text_excerpt', 'structured_object', 'calculation', 'model_explanation', 'manual_note'))
);

CREATE INDEX IF NOT EXISTS idx_emr_rule_evidences_hit_id
  ON emr_rule_evidences (hit_id);

CREATE TABLE IF NOT EXISTS emr_rule_doctor_actions (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  hit_id BIGINT NOT NULL REFERENCES emr_rule_hits(id) ON DELETE CASCADE,
  record_id BIGINT NOT NULL,
  action_type VARCHAR(32) NOT NULL,
  actor_user_id BIGINT NOT NULL,
  actor_role VARCHAR(32) NOT NULL DEFAULT '',
  comment TEXT NOT NULL DEFAULT '',
  before_record_version INT NULL,
  after_record_version INT NULL,
  record_snapshot_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_rule_doctor_actions_type_check CHECK (action_type IN ('locate', 'edit', 'mark_not_applicable', 'acknowledge', 'rerun', 'dismiss_notice'))
);

CREATE INDEX IF NOT EXISTS idx_emr_rule_doctor_actions_hit_id
  ON emr_rule_doctor_actions (hit_id, created_at DESC);

CREATE TABLE IF NOT EXISTS emr_rule_summaries (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  record_id BIGINT NOT NULL,
  record_version INT NULL,
  blocking_count INT NOT NULL DEFAULT 0,
  important_count INT NOT NULL DEFAULT 0,
  notice_count INT NOT NULL DEFAULT 0,
  unhandled_count INT NOT NULL DEFAULT 0,
  acknowledged_count INT NOT NULL DEFAULT 0,
  resolved_count INT NOT NULL DEFAULT 0,
  can_submit BOOLEAN NOT NULL DEFAULT TRUE,
  can_archive BOOLEAN NOT NULL DEFAULT TRUE,
  last_run_id BIGINT NULL,
  last_run_stage VARCHAR(32) NOT NULL DEFAULT '',
  last_checked_at TIMESTAMPTZ NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT uq_emr_rule_summaries_tenant_record UNIQUE (tenant_id, record_id)
);

CREATE INDEX IF NOT EXISTS idx_emr_rule_summaries_tenant_record
  ON emr_rule_summaries (tenant_id, record_id);
