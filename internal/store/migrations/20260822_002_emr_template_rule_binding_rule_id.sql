ALTER TABLE compliance_rules
  ADD COLUMN IF NOT EXISTS rule_source VARCHAR(64) NOT NULL DEFAULT 'platform_builtin',
  ADD COLUMN IF NOT EXISTS applies_to_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS default_stage_config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS action_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS message_doctor TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS message_reviewer TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS score_config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS version_no INTEGER NOT NULL DEFAULT 1;

ALTER TABLE emr_template_rule_bindings
  ADD COLUMN IF NOT EXISTS rule_id TEXT;

UPDATE emr_template_rule_bindings b
SET rule_id = r.id
FROM compliance_rules r
WHERE b.rule_id IS NULL
  AND b.rule_code = r.code
  AND r.deleted_at IS NULL
  AND (r.tenant_id = 0 OR r.tenant_id = b.tenant_id);

CREATE INDEX IF NOT EXISTS idx_emr_template_rule_bindings_tenant_rule
  ON emr_template_rule_bindings (tenant_id, rule_id)
  WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_emr_template_rule_bindings_template_rule_id
  ON emr_template_rule_bindings (template_id, rule_id)
  WHERE deleted_at IS NULL AND rule_id IS NOT NULL;
