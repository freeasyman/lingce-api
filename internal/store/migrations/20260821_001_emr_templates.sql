CREATE TABLE IF NOT EXISTS emr_templates (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  code VARCHAR(128) NOT NULL,
  name TEXT NOT NULL,
  short_name TEXT NOT NULL DEFAULT '',
  department_code TEXT NOT NULL DEFAULT '',
  department_name TEXT NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'enabled',
  description TEXT NOT NULL DEFAULT '',
  version_no INTEGER NOT NULL DEFAULT 1,
  is_system BOOLEAN NOT NULL DEFAULT FALSE,
  schema_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_by BIGINT NULL,
  updated_by BIGINT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ NULL,
  CONSTRAINT chk_emr_templates_status CHECK (status IN ('enabled', 'disabled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_emr_templates_tenant_code
  ON emr_templates (tenant_id, code)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_emr_templates_tenant_status
  ON emr_templates (tenant_id, status)
  WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS emr_template_rule_bindings (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  template_id BIGINT NOT NULL REFERENCES emr_templates(id) ON DELETE CASCADE,
  rule_code VARCHAR(128) NOT NULL,
  rule_name TEXT NOT NULL DEFAULT '',
  rule_detail TEXT NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  rule_scope VARCHAR(32) NOT NULL DEFAULT 'common',
  stage_config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_by BIGINT NULL,
  updated_by BIGINT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ NULL,
  CONSTRAINT chk_emr_template_rule_bindings_scope CHECK (rule_scope IN ('common', 'specialty'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_emr_template_rule_bindings_template_rule_code
  ON emr_template_rule_bindings (template_id, rule_code)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_emr_template_rule_bindings_tenant_template
  ON emr_template_rule_bindings (tenant_id, template_id)
  WHERE deleted_at IS NULL;
