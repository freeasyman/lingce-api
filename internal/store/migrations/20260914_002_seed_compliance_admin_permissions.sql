-- Initialize Compliance Guard access for existing institution administrators.
-- This follows the existing followup/EMR permission seed pattern:
-- institution RBAC identifies the administrator, while this product keeps its
-- own business permission assignment.
-- 署名：Codex，合规卫士开发 Agent
-- 时间：2026-09-14

INSERT INTO compliance_permission_assignments (
  tenant_id,
  employee_id,
  enabled,
  role_code,
  abilities_json,
  created_at,
  updated_at
)
SELECT
  e.tenant_id,
  e.id,
  TRUE,
  'compliance_admin',
  '[
    "compliance.finding.read",
    "compliance.finding.review",
    "compliance.rule.read",
    "compliance.rule.manage",
    "compliance.run.read",
    "compliance.permission.manage"
  ]'::jsonb,
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
