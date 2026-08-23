ALTER TABLE compliance_events
  ADD COLUMN IF NOT EXISTS rule_code VARCHAR(128) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS quote_hash VARCHAR(64) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_compliance_events_tenant_rule_code
  ON compliance_events (tenant_id, rule_code, timestamp DESC)
  WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_compliance_events_tenant_encounter_rule_quote
  ON compliance_events (tenant_id, encounter_id, rule_code, quote_hash)
  WHERE deleted_at IS NULL
    AND rule_code <> ''
    AND quote_hash <> '';
