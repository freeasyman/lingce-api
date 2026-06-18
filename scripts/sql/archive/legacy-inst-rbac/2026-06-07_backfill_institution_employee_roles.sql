-- Backfill institution_employee_roles from inst_employee_roles first, then legacy institution_employee_roles
BEGIN;

WITH ranked_inst AS (
  SELECT
    ier.tenant_id,
    ier.employee_id,
    LOWER(TRIM(ier.role_code)) AS role_code,
    COALESCE(NULLIF(TRIM(ier.source), ''), 'inst_backfill') AS source,
    ROW_NUMBER() OVER (
      PARTITION BY ier.tenant_id, ier.employee_id
      ORDER BY CASE LOWER(TRIM(ier.source))
        WHEN 'manual' THEN 1
        WHEN 'department' THEN 2
        WHEN 'tenant_init' THEN 3
        ELSE 9
      END,
      ier.created_at DESC NULLS LAST,
      ier.employee_id DESC
    ) AS rn
  FROM inst_employee_roles ier
  WHERE TRIM(COALESCE(ier.role_code, '')) <> ''
), normalized_inst AS (
  SELECT
    src.tenant_id,
    src.employee_id,
    src.role_code,
    src.source,
    ir.id AS role_id
  FROM ranked_inst src
  LEFT JOIN institution_roles ir
    ON ir.tenant_id = src.tenant_id
   AND LOWER(TRIM(ir.code)) = src.role_code
   AND ir.deleted_at IS NULL
  WHERE src.rn = 1
    AND src.role_code NOT IN ('lingce_sales', 'inst_test_225052')
    AND ir.id IS NOT NULL
)
UPDATE institution_employee_roles dst
SET tenant_id = src.tenant_id,
    role_id = src.role_id,
    role_code = src.role_code,
    source = src.source,
    updated_at = NOW()
FROM normalized_inst src
WHERE dst.employee_id = src.employee_id;

WITH ranked_inst AS (
  SELECT
    ier.tenant_id,
    ier.employee_id,
    LOWER(TRIM(ier.role_code)) AS role_code,
    COALESCE(NULLIF(TRIM(ier.source), ''), 'inst_backfill') AS source,
    ROW_NUMBER() OVER (
      PARTITION BY ier.tenant_id, ier.employee_id
      ORDER BY CASE LOWER(TRIM(ier.source))
        WHEN 'manual' THEN 1
        WHEN 'department' THEN 2
        WHEN 'tenant_init' THEN 3
        ELSE 9
      END,
      ier.created_at DESC NULLS LAST,
      ier.employee_id DESC
    ) AS rn
  FROM inst_employee_roles ier
  WHERE TRIM(COALESCE(ier.role_code, '')) <> ''
), normalized_inst AS (
  SELECT
    src.tenant_id,
    src.employee_id,
    src.role_code,
    src.source,
    ir.id AS role_id
  FROM ranked_inst src
  LEFT JOIN institution_roles ir
    ON ir.tenant_id = src.tenant_id
   AND LOWER(TRIM(ir.code)) = src.role_code
   AND ir.deleted_at IS NULL
  WHERE src.rn = 1
    AND src.role_code NOT IN ('lingce_sales', 'inst_test_225052')
    AND ir.id IS NOT NULL
)
INSERT INTO institution_employee_roles (
  employee_id, role_id, tenant_id, role_code, source, created_at, updated_at
)
SELECT
  src.employee_id,
  src.role_id,
  src.tenant_id,
  src.role_code,
  src.source,
  NOW(),
  NOW()
FROM normalized_inst src
LEFT JOIN institution_employee_roles dst
  ON dst.employee_id = src.employee_id
WHERE dst.employee_id IS NULL;

WITH legacy_rows AS (
  SELECT
    e.tenant_id,
    ier.employee_id,
    LOWER(TRIM(ir.code)) AS role_code,
    ir.id AS role_id
  FROM institution_employee_roles ier
  JOIN employees e ON e.id = ier.employee_id
  LEFT JOIN institution_roles ir ON ir.id = ier.role_id
  WHERE ier.tenant_id IS NULL OR TRIM(COALESCE(ier.role_code, '')) = ''
)
UPDATE institution_employee_roles dst
SET tenant_id = src.tenant_id,
    role_id = COALESCE(dst.role_id, src.role_id),
    role_code = src.role_code,
    source = COALESCE(NULLIF(TRIM(dst.source), ''), 'legacy_backfill'),
    updated_at = NOW()
FROM legacy_rows src
WHERE dst.employee_id = src.employee_id;

COMMIT;
