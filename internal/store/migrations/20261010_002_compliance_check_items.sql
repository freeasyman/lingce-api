-- +goose Up
CREATE TABLE IF NOT EXISTS compliance_check_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  subject VARCHAR(32) NOT NULL,
  source_type VARCHAR(48) NOT NULL,
  source_id VARCHAR(128) NOT NULL,
  source_version VARCHAR(128) NOT NULL,
  scene_code VARCHAR(128) NOT NULL,
  role_code VARCHAR(64) NOT NULL DEFAULT '',
  employee_id BIGINT,
  theme_key VARCHAR(160) NOT NULL,
  question TEXT NOT NULL,
  risk_hypothesis TEXT NOT NULL,
  primary_risk_type VARCHAR(64) NOT NULL,
  related_risk_types JSONB NOT NULL DEFAULT '[]'::jsonb,
  evidence JSONB NOT NULL DEFAULT '[]'::jsonb,
  check_basis JSONB NOT NULL DEFAULT '[]'::jsonb,
  suggested_actions JSONB NOT NULL DEFAULT '[]'::jsonb,
  priority VARCHAR(16) NOT NULL,
  status VARCHAR(24) NOT NULL DEFAULT 'pending',
  started_by BIGINT,
  started_at TIMESTAMPTZ,
  processed_by BIGINT,
  processed_at TIMESTAMPTZ,
  conclusion VARCHAR(64),
  process_reason TEXT,
  process_actions JSONB NOT NULL DEFAULT '[]'::jsonb,
  process_note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT compliance_check_items_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT compliance_check_items_status_check CHECK (status IN ('pending','in_progress','processed')),
  CONSTRAINT compliance_check_items_priority_check CHECK (priority IN ('urgent','important','normal')),
  CONSTRAINT compliance_check_items_risk_check CHECK (primary_risk_type IN ('patient_decision','medical_safety','legal_evidence','business_reputation','management_system')),
  CONSTRAINT compliance_check_items_related_types_check CHECK (jsonb_typeof(related_risk_types) = 'array'),
  CONSTRAINT compliance_check_items_evidence_check CHECK (jsonb_typeof(evidence) = 'array'),
  CONSTRAINT compliance_check_items_basis_check CHECK (jsonb_typeof(check_basis) = 'array'),
  CONSTRAINT compliance_check_items_actions_check CHECK (jsonb_typeof(suggested_actions) = 'array'),
  UNIQUE (tenant_id, source_type, source_id, source_version, theme_key)
);
CREATE TABLE IF NOT EXISTS compliance_check_item_sources (
  item_id UUID NOT NULL REFERENCES compliance_check_items(id) ON DELETE CASCADE,
  finding_id UUID NOT NULL REFERENCES compliance_findings(id) ON DELETE CASCADE,
  run_id UUID NOT NULL REFERENCES compliance_check_runs(id) ON DELETE CASCADE,
  rule_code VARCHAR(128) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (item_id, finding_id)
);
CREATE TABLE IF NOT EXISTS compliance_check_item_process_records (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  item_id UUID NOT NULL REFERENCES compliance_check_items(id) ON DELETE CASCADE,
  tenant_id BIGINT NOT NULL,
  actor_id BIGINT NOT NULL,
  idempotency_key VARCHAR(256) NOT NULL,
  conclusion VARCHAR(64) NOT NULL,
  reason TEXT NOT NULL,
  actions JSONB NOT NULL DEFAULT '[]'::jsonb,
  note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (item_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS compliance_check_items_tenant_status_idx ON compliance_check_items (tenant_id,status,created_at DESC);
CREATE INDEX IF NOT EXISTS compliance_check_item_sources_finding_idx ON compliance_check_item_sources (finding_id);

-- +goose Down
DROP TABLE IF EXISTS compliance_check_item_process_records;
DROP TABLE IF EXISTS compliance_check_item_sources;
DROP TABLE IF EXISTS compliance_check_items;
