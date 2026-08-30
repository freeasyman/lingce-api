CREATE TABLE IF NOT EXISTS emr_permission_assignments (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  employee_id BIGINT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  emr_role_code VARCHAR(64) NOT NULL DEFAULT 'readonly',
  scope VARCHAR(32) NOT NULL DEFAULT 'self',
  abilities_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_by BIGINT NULL,
  updated_by BIGINT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT uq_emr_permission_assignments_tenant_employee UNIQUE (tenant_id, employee_id),
  CONSTRAINT chk_emr_permission_scope CHECK (scope IN ('self', 'department', 'tenant')),
  CONSTRAINT chk_emr_permission_role_code CHECK (emr_role_code IN ('emr_admin', 'doctor', 'archivist', 'readonly'))
);

CREATE INDEX IF NOT EXISTS idx_emr_permission_assignments_tenant_id
  ON emr_permission_assignments (tenant_id);

CREATE INDEX IF NOT EXISTS idx_emr_permission_assignments_employee_id
  ON emr_permission_assignments (employee_id);
