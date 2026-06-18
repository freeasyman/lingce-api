-- Backfill institution_role_menus from inst_role_menus + inst_menus
BEGIN;

INSERT INTO institution_role_menus (
  tenant_id, role_code, menu_code, created_at, updated_at
)
SELECT DISTINCT
  rm.tenant_id,
  LOWER(TRIM(rm.role_code)) AS role_code,
  LOWER(TRIM(m.code)) AS menu_code,
  COALESCE(rm.created_at, NOW()) AS created_at,
  NOW() AS updated_at
FROM inst_role_menus rm
JOIN inst_menus m ON m.id = rm.menu_id
WHERE TRIM(COALESCE(rm.role_code, '')) <> ''
  AND TRIM(COALESCE(m.code, '')) <> ''
ON CONFLICT DO NOTHING;

COMMIT;
