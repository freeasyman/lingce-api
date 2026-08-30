-- Shared Encounter fact table.
-- The table is owned by the shared recording/compliance domain. EMR stores
-- encounter_id as a reference and does not create or govern Encounter rows.

CREATE TABLE IF NOT EXISTS encounters (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  source_type VARCHAR(32) NOT NULL,
  source_id BIGINT NOT NULL,
  patient_id BIGINT,
  patient_name VARCHAR(255),
  patient_candidate_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  provider_id BIGINT,
  provider_name VARCHAR(128) NOT NULL DEFAULT '',
  provider_role VARCHAR(64) NOT NULL DEFAULT '',
  department_id BIGINT,
  channel VARCHAR(32) NOT NULL DEFAULT 'store',
  visit_type VARCHAR(32) NOT NULL DEFAULT 'consultation',
  started_at TIMESTAMPTZ,
  ended_at TIMESTAMPTZ,
  status VARCHAR(32) NOT NULL DEFAULT 'new',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE encounters ADD COLUMN IF NOT EXISTS patient_name VARCHAR(255);
ALTER TABLE encounters ADD COLUMN IF NOT EXISTS patient_candidate_json JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE encounters ADD COLUMN IF NOT EXISTS provider_name VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE encounters ADD COLUMN IF NOT EXISTS provider_role VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE encounters ADD COLUMN IF NOT EXISTS channel VARCHAR(32) NOT NULL DEFAULT 'store';
ALTER TABLE encounters ADD COLUMN IF NOT EXISTS visit_type VARCHAR(32) NOT NULL DEFAULT 'consultation';
ALTER TABLE encounters ADD COLUMN IF NOT EXISTS status VARCHAR(32) NOT NULL DEFAULT 'new';
ALTER TABLE encounters ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE encounters ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE UNIQUE INDEX IF NOT EXISTS idx_encounters_tenant_source
  ON encounters (tenant_id, source_type, source_id);

CREATE INDEX IF NOT EXISTS idx_encounters_tenant_status
  ON encounters (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_encounters_tenant_patient
  ON encounters (tenant_id, patient_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_encounters_tenant_provider
  ON encounters (tenant_id, provider_id, started_at DESC);
