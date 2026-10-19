-- 患者运营权限：全部员工待办 / 权限管理。
CREATE TABLE IF NOT EXISTS patient_ops_permission_assignments (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    employee_id BIGINT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    role_code VARCHAR(64) NOT NULL DEFAULT 'patient_ops_staff',
    abilities_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by BIGINT NULL,
    updated_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_patient_ops_permission_tenant_employee UNIQUE (tenant_id, employee_id),
    CONSTRAINT chk_patient_ops_permission_role_code CHECK (role_code IN ('patient_ops_admin', 'patient_ops_staff'))
);

CREATE INDEX IF NOT EXISTS idx_patient_ops_permission_tenant_id
    ON patient_ops_permission_assignments (tenant_id);

CREATE INDEX IF NOT EXISTS idx_patient_ops_permission_employee_id
    ON patient_ops_permission_assignments (employee_id);

-- 机构管理员默认开通“全部员工待办”和“权限管理”，其他员工由管理员在权限页分配。
INSERT INTO patient_ops_permission_assignments (
    tenant_id, employee_id, enabled, role_code, abilities_json, created_at, updated_at
)
SELECT
    e.tenant_id,
    e.id,
    TRUE,
    'patient_ops_admin',
    '["patient_ops.tasks.read_all", "patient_ops.permission.manage"]'::jsonb,
    NOW(),
    NOW()
FROM employees e
WHERE e.deleted_at IS NULL
  AND e.is_active::text IN ('1', 't', 'true', 'TRUE', 'yes')
  AND EXISTS (
      SELECT 1
      FROM institution_employee_roles er
      WHERE er.tenant_id = e.tenant_id
        AND er.employee_id = e.id
        AND LOWER(TRIM(er.role_code)) = 'admin'
  )
ON CONFLICT (tenant_id, employee_id) DO NOTHING;
