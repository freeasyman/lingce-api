-- Institution menu permission unification audit
-- Purpose:
-- 1) Inventory current table existence and row counts
-- 2) Detect dual-source divergence
-- 3) Detect package/role/menu cross-layer inconsistencies
-- 4) Produce migration pre-check evidence
--
-- Run manually against dev / test / prod and save outputs with environment labels.

BEGIN;

-- =========================================================
-- A. Table existence inventory
-- =========================================================

SELECT
  'institution_employee_roles' AS table_name,
  to_regclass('public.institution_employee_roles') IS NOT NULL AS exists
UNION ALL
SELECT 'inst_employee_roles', to_regclass('public.inst_employee_roles') IS NOT NULL
UNION ALL
SELECT 'institution_roles', to_regclass('public.institution_roles') IS NOT NULL
UNION ALL
SELECT 'inst_roles', to_regclass('public.inst_roles') IS NOT NULL
UNION ALL
SELECT 'institution_menus', to_regclass('public.institution_menus') IS NOT NULL
UNION ALL
SELECT 'inst_menus', to_regclass('public.inst_menus') IS NOT NULL
UNION ALL
SELECT 'institution_role_menus', to_regclass('public.institution_role_menus') IS NOT NULL
UNION ALL
SELECT 'inst_role_menus', to_regclass('public.inst_role_menus') IS NOT NULL
UNION ALL
SELECT 'institution_department_roles', to_regclass('public.institution_department_roles') IS NOT NULL
UNION ALL
SELECT 'inst_department_roles', to_regclass('public.inst_department_roles') IS NOT NULL
UNION ALL
SELECT 'tenant_feature_groups', to_regclass('public.tenant_feature_groups') IS NOT NULL
UNION ALL
SELECT 'tenant_feature_group_items', to_regclass('public.tenant_feature_group_items') IS NOT NULL
UNION ALL
SELECT 'tenant_feature_assignments', to_regclass('public.tenant_feature_assignments') IS NOT NULL
UNION ALL
SELECT 'tenant_feature_overrides', to_regclass('public.tenant_feature_overrides') IS NOT NULL
ORDER BY table_name;

-- =========================================================
-- B. Current row counts
-- =========================================================

SELECT 'institution_employee_roles' AS table_name, COUNT(*) AS row_count FROM institution_employee_roles
UNION ALL
SELECT 'inst_employee_roles', COUNT(*) FROM inst_employee_roles
UNION ALL
SELECT 'institution_roles', COUNT(*) FROM institution_roles
UNION ALL
SELECT 'inst_roles', COUNT(*) FROM inst_roles
UNION ALL
SELECT 'inst_menus', COUNT(*) FROM inst_menus
UNION ALL
SELECT 'inst_role_menus', COUNT(*) FROM inst_role_menus
UNION ALL
SELECT 'institution_department_roles', COUNT(*) FROM institution_department_roles
UNION ALL
SELECT 'inst_department_roles', COUNT(*) FROM inst_department_roles
UNION ALL
SELECT 'tenant_feature_groups', COUNT(*) FROM tenant_feature_groups
UNION ALL
SELECT 'tenant_feature_group_items', COUNT(*) FROM tenant_feature_group_items
UNION ALL
SELECT 'tenant_feature_assignments', COUNT(*) FROM tenant_feature_assignments
UNION ALL
SELECT 'tenant_feature_overrides', COUNT(*) FROM tenant_feature_overrides
ORDER BY table_name;

-- =========================================================
-- C. Employee role dual-source divergence
-- =========================================================

WITH institution_side AS (
  SELECT
    ier.employee_id,
    e.tenant_id,
    LOWER(TRIM(COALESCE(ir.code, ''))) AS role_code
  FROM institution_employee_roles ier
  JOIN employees e ON e.id = ier.employee_id
  LEFT JOIN institution_roles ir ON ir.id = ier.role_id
),
inst_side AS (
  SELECT
    employee_id,
    tenant_id,
    LOWER(TRIM(COALESCE(role_code, ''))) AS role_code
  FROM inst_employee_roles
),
joined AS (
  SELECT
    COALESCE(i.tenant_id, s.tenant_id) AS tenant_id,
    COALESCE(i.employee_id, s.employee_id) AS employee_id,
    i.role_code AS institution_role_code,
    s.role_code AS inst_role_code
  FROM institution_side i
  FULL OUTER JOIN inst_side s
    ON s.tenant_id = i.tenant_id
   AND s.employee_id = i.employee_id
)
SELECT
  COUNT(*) FILTER (WHERE institution_role_code IS NOT NULL) AS institution_rows,
  COUNT(*) FILTER (WHERE inst_role_code IS NOT NULL) AS inst_rows,
  COUNT(*) FILTER (
    WHERE institution_role_code IS NOT NULL
      AND inst_role_code IS NOT NULL
      AND institution_role_code = inst_role_code
  ) AS exact_matches,
  COUNT(*) FILTER (
    WHERE institution_role_code IS NOT NULL
      AND inst_role_code IS NOT NULL
      AND institution_role_code <> inst_role_code
  ) AS mismatched_pairs,
  COUNT(*) FILTER (WHERE institution_role_code IS NOT NULL AND inst_role_code IS NULL) AS institution_only,
  COUNT(*) FILTER (WHERE institution_role_code IS NULL AND inst_role_code IS NOT NULL) AS inst_only
FROM joined;

WITH inst_multi AS (
  SELECT tenant_id, employee_id, ARRAY_AGG(role_code ORDER BY role_code) AS role_codes
  FROM inst_employee_roles
  GROUP BY tenant_id, employee_id
  HAVING COUNT(*) > 1
)
SELECT *
FROM inst_multi
ORDER BY tenant_id, employee_id;

SELECT
  ier.tenant_id,
  ier.employee_id,
  ier.role_code
FROM inst_employee_roles ier
LEFT JOIN institution_roles ir
  ON ir.tenant_id = ier.tenant_id
 AND LOWER(TRIM(ir.code)) = LOWER(TRIM(ier.role_code))
 AND ir.deleted_at IS NULL
WHERE ir.id IS NULL
ORDER BY ier.tenant_id, ier.employee_id, ier.role_code;

-- =========================================================
-- D. Role definition divergence
-- =========================================================

WITH institution_codes AS (
  SELECT tenant_id, LOWER(TRIM(code)) AS code
  FROM institution_roles
  WHERE deleted_at IS NULL
),
inst_codes AS (
  SELECT tenant_id, LOWER(TRIM(code)) AS code
  FROM inst_roles
),
joined AS (
  SELECT
    COALESCE(i.tenant_id, s.tenant_id) AS tenant_id,
    i.code AS institution_code,
    s.code AS inst_code
  FROM institution_codes i
  FULL OUTER JOIN inst_codes s
    ON s.tenant_id = i.tenant_id
   AND s.code = i.code
)
SELECT
  tenant_id,
  COUNT(*) FILTER (WHERE institution_code IS NOT NULL AND inst_code IS NOT NULL) AS shared_codes,
  COUNT(*) FILTER (WHERE institution_code IS NOT NULL AND inst_code IS NULL) AS institution_only_codes,
  COUNT(*) FILTER (WHERE institution_code IS NULL AND inst_code IS NOT NULL) AS inst_only_codes
FROM joined
GROUP BY tenant_id
ORDER BY tenant_id;

-- =========================================================
-- E. Menu dictionary and role-menu binding health
-- =========================================================

SELECT
  rm.tenant_id,
  rm.role_code,
  COUNT(*) AS bound_rows,
  COUNT(*) FILTER (WHERE m.id IS NULL) AS missing_menu_rows
FROM inst_role_menus rm
LEFT JOIN inst_menus m ON m.id = rm.menu_id
GROUP BY rm.tenant_id, rm.role_code
ORDER BY rm.tenant_id, rm.role_code;

SELECT
  id,
  name,
  code,
  parent_id,
  order_index,
  feature_code,
  feature_name,
  is_feature_assignable,
  is_default_for_admin
FROM inst_menus
ORDER BY order_index, id;

-- =========================================================
-- F. Tenant feature layer health
-- =========================================================

SELECT
  t.id AS tenant_id,
  t.name AS tenant_name,
  a.group_id,
  g.name AS group_name,
  CASE
    WHEN a.group_id IS NULL THEN 'unassigned'
    WHEN g.id IS NULL THEN 'dangling_assignment'
    ELSE 'assigned'
  END AS assignment_state
FROM tenants t
LEFT JOIN tenant_feature_assignments a ON a.tenant_id = t.id
LEFT JOIN tenant_feature_groups g ON g.id = a.group_id
ORDER BY t.id;

SELECT
  a.tenant_id,
  COUNT(*) FILTER (
    WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
  ) AS menu_items,
  COUNT(*) FILTER (
    WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
      AND COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')) = ''
  ) AS empty_menu_codes
FROM tenant_feature_assignments a
LEFT JOIN tenant_feature_group_items i ON i.group_id = a.group_id
GROUP BY a.tenant_id
ORDER BY a.tenant_id;

SELECT
  o.tenant_id,
  COALESCE(NULLIF(o.item_type, ''), 'feature') AS item_type,
  COALESCE(NULLIF(o.item_code, ''), COALESCE(o.feature_code, '')) AS item_code,
  COALESCE(NULLIF(o.override_mode, ''), CASE WHEN COALESCE(o.is_enabled, true) THEN 'allow' ELSE 'deny' END) AS override_mode
FROM tenant_feature_overrides o
ORDER BY o.tenant_id, item_type, item_code;

-- =========================================================
-- G. Cross-layer tenant/package/role inconsistencies
-- =========================================================

WITH tenant_allowed AS (
  SELECT
    a.tenant_id,
    LOWER(TRIM(COALESCE(NULLIF(i.item_code, ''), COALESCE(i.feature_code, '')))) AS menu_code
  FROM tenant_feature_assignments a
  JOIN tenant_feature_group_items i ON i.group_id = a.group_id
  WHERE COALESCE(NULLIF(i.item_type, ''), 'feature') = 'menu'
    AND COALESCE(i.is_enabled, true) = true
),
role_grants AS (
  SELECT
    rm.tenant_id,
    rm.role_code,
    LOWER(TRIM(m.code)) AS menu_code
  FROM inst_role_menus rm
  JOIN inst_menus m ON m.id = rm.menu_id
)
SELECT
  rg.tenant_id,
  rg.role_code,
  rg.menu_code
FROM role_grants rg
LEFT JOIN tenant_allowed ta
  ON ta.tenant_id = rg.tenant_id
 AND ta.menu_code = rg.menu_code
WHERE ta.menu_code IS NULL
ORDER BY rg.tenant_id, rg.role_code, rg.menu_code;

-- =========================================================
-- H. Migration readiness summary
-- =========================================================

SELECT
  COUNT(*) AS tenants_total,
  COUNT(*) FILTER (WHERE group_id IS NULL) AS tenants_without_feature_group
FROM tenant_feature_assignments
RIGHT JOIN tenants t ON t.id = tenant_feature_assignments.tenant_id;

SELECT
  COUNT(*) AS inst_employee_role_multi_rows
FROM (
  SELECT tenant_id, employee_id
  FROM inst_employee_roles
  GROUP BY tenant_id, employee_id
  HAVING COUNT(*) > 1
) x;

SELECT
  COUNT(*) AS inst_unmapped_role_codes
FROM (
  SELECT DISTINCT ier.tenant_id, LOWER(TRIM(ier.role_code)) AS role_code
  FROM inst_employee_roles ier
  LEFT JOIN institution_roles ir
    ON ir.tenant_id = ier.tenant_id
   AND LOWER(TRIM(ir.code)) = LOWER(TRIM(ier.role_code))
   AND ir.deleted_at IS NULL
  WHERE ir.id IS NULL
) x;

ROLLBACK;
