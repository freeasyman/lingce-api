-- Backfill institution_menus from inst_menus
BEGIN;

INSERT INTO institution_menus (
  tenant_id, code, name, path, icon, parent_id, parent_code, sort_order, is_active,
  is_feature_assignable, is_default_for_admin, feature_code, feature_name,
  created_at, updated_at, deleted_at
)
SELECT
  NULL::bigint AS tenant_id,
  LOWER(TRIM(m.code)) AS code,
  COALESCE(NULLIF(TRIM(m.name), ''), NULLIF(TRIM(m.code), ''), 'unknown') AS name,
  m.path,
  m.icon,
  m.parent_id,
  LOWER(TRIM(pm.code)) AS parent_code,
  COALESCE(m.order_index, 0) AS sort_order,
  COALESCE(m.is_active, true) AS is_active,
  COALESCE(m.is_feature_assignable, false) AS is_feature_assignable,
  COALESCE(m.is_default_for_admin, false) AS is_default_for_admin,
  NULLIF(TRIM(m.feature_code), '') AS feature_code,
  NULLIF(TRIM(m.feature_name), '') AS feature_name,
  COALESCE(m.created_at, NOW()) AS created_at,
  COALESCE(m.created_at, NOW()) AS updated_at,
  NULL::timestamp without time zone AS deleted_at
FROM inst_menus m
LEFT JOIN inst_menus pm ON pm.id = m.parent_id
WHERE TRIM(COALESCE(m.code, '')) <> ''
ON CONFLICT DO NOTHING;

UPDATE institution_menus im
SET name = src.name,
    path = src.path,
    icon = src.icon,
    parent_id = src.parent_id,
    parent_code = src.parent_code,
    sort_order = src.sort_order,
    is_active = src.is_active,
    is_feature_assignable = src.is_feature_assignable,
    is_default_for_admin = src.is_default_for_admin,
    feature_code = src.feature_code,
    feature_name = src.feature_name,
    updated_at = NOW()
FROM (
  SELECT
    LOWER(TRIM(m.code)) AS code,
    COALESCE(NULLIF(TRIM(m.name), ''), NULLIF(TRIM(m.code), ''), 'unknown') AS name,
    m.path,
    m.icon,
    m.parent_id,
    LOWER(TRIM(pm.code)) AS parent_code,
    COALESCE(m.order_index, 0) AS sort_order,
    COALESCE(m.is_active, true) AS is_active,
    COALESCE(m.is_feature_assignable, false) AS is_feature_assignable,
    COALESCE(m.is_default_for_admin, false) AS is_default_for_admin,
    NULLIF(TRIM(m.feature_code), '') AS feature_code,
    NULLIF(TRIM(m.feature_name), '') AS feature_name
  FROM inst_menus m
  LEFT JOIN inst_menus pm ON pm.id = m.parent_id
  WHERE TRIM(COALESCE(m.code, '')) <> ''
) src
WHERE im.tenant_id IS NULL
  AND LOWER(TRIM(im.code)) = src.code
  AND im.deleted_at IS NULL;

COMMIT;
