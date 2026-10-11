-- Compliance Guard business permissions.
-- This table belongs to Compliance Guard only; it does not replace institution RBAC.
-- 署名：Codex，合规卫士开发 Agent
-- 时间：2026-09-14

CREATE TABLE IF NOT EXISTS compliance_permission_assignments (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  employee_id BIGINT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  role_code VARCHAR(64) NOT NULL DEFAULT 'compliance_staff',
  abilities_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_by BIGINT,
  updated_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT uq_compliance_permission_tenant_employee UNIQUE (tenant_id, employee_id),
  CONSTRAINT chk_compliance_permission_role_code
    CHECK (role_code IN ('compliance_admin', 'compliance_staff')),
  CONSTRAINT chk_compliance_permission_abilities_array
    CHECK (jsonb_typeof(abilities_json) = 'array')
);

CREATE INDEX IF NOT EXISTS idx_compliance_permission_tenant_id
  ON compliance_permission_assignments (tenant_id);

CREATE INDEX IF NOT EXISTS idx_compliance_permission_employee_id
  ON compliance_permission_assignments (employee_id);
