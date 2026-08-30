INSERT INTO emr_permission_assignments (
  tenant_id,
  employee_id,
  enabled,
  emr_role_code,
  scope,
  abilities_json,
  created_at,
  updated_at
)
SELECT
  e.tenant_id,
  e.id,
  TRUE,
  'emr_admin',
  'tenant',
  '["record.read", "record.read_all", "record.create", "record.edit", "record.submit", "record.archive", "record.history.read", "record.export", "template.manage", "quality.manage"]'::jsonb,
  NOW(),
  NOW()
FROM employees e
WHERE e.deleted_at IS NULL
  AND e.is_active = 1
  AND EXISTS (
    SELECT 1
    FROM institution_employee_roles er
    WHERE er.tenant_id = e.tenant_id
      AND er.employee_id = e.id
      AND LOWER(TRIM(er.role_code)) = 'admin'
  )
ON CONFLICT (tenant_id, employee_id) DO NOTHING;
