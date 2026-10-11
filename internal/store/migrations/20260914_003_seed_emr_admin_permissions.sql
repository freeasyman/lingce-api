-- Backfill full EMR permissions for all institution administrators.
-- 署名：Codex
-- 时间：2026-09-14

INSERT INTO emr_permission_assignments (
  tenant_id, employee_id, enabled, emr_role_code, scope, abilities_json, created_at, updated_at
)
SELECT
  e.tenant_id, e.id, TRUE, 'emr_admin', 'tenant',
  '["record.read", "record.read_all", "record.create", "record.edit", "record.submit", "record.archive", "record.history.read", "record.export", "template.manage", "quality.manage"]'::jsonb,
  NOW(), NOW()
FROM employees e
WHERE e.deleted_at IS NULL
  AND lower(COALESCE(e.is_active::text, 'true')) IN ('1', 't', 'true', 'yes')
  AND EXISTS (
    SELECT 1 FROM institution_employee_roles er
    WHERE er.tenant_id = e.tenant_id
      AND er.employee_id = e.id
      AND LOWER(TRIM(er.role_code)) = 'admin'
  )
ON CONFLICT (tenant_id, employee_id)
DO UPDATE SET
  enabled = TRUE,
  emr_role_code = 'emr_admin',
  scope = 'tenant',
  abilities_json = EXCLUDED.abilities_json,
  updated_at = NOW();
