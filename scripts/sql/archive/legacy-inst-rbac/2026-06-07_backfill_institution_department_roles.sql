-- Backfill institution_department_roles from inst_department_roles and existing role links
BEGIN;

WITH legacy_rows AS (
  SELECT
    s.department_id,
    s.tenant_id,
    LOWER(TRIM(s.role_code)) AS role_code,
    ir.id AS role_id
  FROM inst_department_roles s
  LEFT JOIN institution_roles ir
    ON ir.tenant_id = s.tenant_id
   AND LOWER(TRIM(ir.code)) = LOWER(TRIM(s.role_code))
   AND ir.deleted_at IS NULL
  WHERE TRIM(COALESCE(s.role_code, '')) <> ''
    AND LOWER(TRIM(s.role_code)) NOT IN ('lingce_sales', 'inst_test_225052')
    AND ir.id IS NOT NULL
)
UPDATE institution_department_roles dst
SET tenant_id = src.tenant_id,
    role_id = src.role_id,
    role_code = src.role_code,
    is_default = true,
    updated_at = NOW()
FROM legacy_rows src
WHERE dst.department_id = src.department_id;

WITH legacy_rows AS (
  SELECT
    s.department_id,
    s.tenant_id,
    LOWER(TRIM(s.role_code)) AS role_code,
    ir.id AS role_id
  FROM inst_department_roles s
  LEFT JOIN institution_roles ir
    ON ir.tenant_id = s.tenant_id
   AND LOWER(TRIM(ir.code)) = LOWER(TRIM(s.role_code))
   AND ir.deleted_at IS NULL
  WHERE TRIM(COALESCE(s.role_code, '')) <> ''
    AND LOWER(TRIM(s.role_code)) NOT IN ('lingce_sales', 'inst_test_225052')
    AND ir.id IS NOT NULL
)
INSERT INTO institution_department_roles (
  department_id, role_id, tenant_id, role_code, is_default, created_at, updated_at
)
SELECT
  src.department_id,
  src.role_id,
  src.tenant_id,
  src.role_code,
  true,
  NOW(),
  NOW()
FROM legacy_rows src
LEFT JOIN institution_department_roles dst
  ON dst.department_id = src.department_id
WHERE dst.department_id IS NULL;

WITH unresolved_new_rows AS (
  SELECT
    dst.department_id,
    d.tenant_id,
    LOWER(TRIM(ir.code)) AS role_code,
    ir.id AS role_id
  FROM institution_department_roles dst
  JOIN departments d ON d.id = dst.department_id
  LEFT JOIN institution_roles ir ON ir.id = dst.role_id
  WHERE (dst.tenant_id IS NULL OR TRIM(COALESCE(dst.role_code, '')) = '')
    AND ir.id IS NOT NULL
)
UPDATE institution_department_roles dst
SET tenant_id = src.tenant_id,
    role_id = COALESCE(dst.role_id, src.role_id),
    role_code = src.role_code,
    updated_at = NOW()
FROM unresolved_new_rows src
WHERE dst.department_id = src.department_id;

COMMIT;
