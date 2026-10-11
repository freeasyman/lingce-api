CREATE TABLE IF NOT EXISTS followup_permission_assignments (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  employee_id BIGINT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  role_code VARCHAR(64) NOT NULL DEFAULT 'followup_staff',
  abilities_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_by BIGINT NULL,
  updated_by BIGINT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT uq_followup_permission_tenant_employee UNIQUE (tenant_id, employee_id),
  CONSTRAINT chk_followup_permission_role_code CHECK (role_code IN ('followup_admin', 'followup_staff'))
);

CREATE INDEX IF NOT EXISTS idx_followup_permission_tenant_id
  ON followup_permission_assignments (tenant_id);

CREATE INDEX IF NOT EXISTS idx_followup_permission_employee_id
  ON followup_permission_assignments (employee_id);

INSERT INTO followup_permission_assignments (
  tenant_id, employee_id, enabled, role_code, abilities_json, created_at, updated_at
)
SELECT
  e.tenant_id,
  e.id,
  TRUE,
  'followup_admin',
  '["followup.task.read", "followup.task.execute", "followup.task.assign", "followup.partnership.manage", "followup.scope.manage", "followup.permission.manage"]'::jsonb,
  NOW(),
  NOW()
FROM employees e
WHERE e.deleted_at IS NULL
  AND e.is_active::text IN ('1','t','true','TRUE','yes')
  AND EXISTS (
    SELECT 1
    FROM institution_employee_roles er
    WHERE er.tenant_id = e.tenant_id
      AND er.employee_id = e.id
      AND LOWER(TRIM(er.role_code)) = 'admin'
  )
ON CONFLICT (tenant_id, employee_id) DO NOTHING;
