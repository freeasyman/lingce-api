CREATE TABLE IF NOT EXISTS emr_document_versions (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  document_id BIGINT NOT NULL,
  version_no INT NOT NULL,
  version_kind VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL,
  change_summary TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  operator_id BIGINT NULL,
  operator_name TEXT NOT NULL DEFAULT '',
  full_document_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  full_snapshot_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT uq_emr_document_versions_document_version UNIQUE (document_id, version_no)
);

CREATE INDEX IF NOT EXISTS idx_emr_document_versions_tenant_id
  ON emr_document_versions (tenant_id);

CREATE INDEX IF NOT EXISTS idx_emr_document_versions_document_id
  ON emr_document_versions (document_id, version_no DESC);

CREATE TABLE IF NOT EXISTS emr_field_events (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  document_id BIGINT NOT NULL,
  version_id BIGINT NOT NULL,
  field_key TEXT NOT NULL,
  action_type VARCHAR(64) NOT NULL,
  before_value TEXT NOT NULL DEFAULT '',
  after_value TEXT NOT NULL DEFAULT '',
  change_reason TEXT NOT NULL DEFAULT '',
  operator_id BIGINT NULL,
  operator_name TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_emr_field_events_tenant_id
  ON emr_field_events (tenant_id);

CREATE INDEX IF NOT EXISTS idx_emr_field_events_document_id
  ON emr_field_events (document_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_emr_field_events_version_id
  ON emr_field_events (version_id);
