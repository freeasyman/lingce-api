-- Backfill institution_roles from global inst_roles template
BEGIN;

INSERT INTO institution_roles (
  tenant_id, code, name, description, is_active, created_at, updated_at, deleted_at
)
SELECT
  0 AS tenant_id,
  LOWER(TRIM(r.code)) AS code,
  COALESCE(NULLIF(TRIM(r.name_cn), ''), NULLIF(TRIM(r.code), ''), 'unknown') AS name,
  NULLIF(TRIM(r.description), '') AS description,
  COALESCE(r.is_active, true) AS is_active,
  COALESCE(r.created_at, NOW()) AS created_at,
  COALESCE(r.updated_at, r.created_at, NOW()) AS updated_at,
  NULL::timestamp without time zone AS deleted_at
FROM inst_roles r
WHERE TRIM(COALESCE(r.code, '')) <> ''
ON CONFLICT DO NOTHING;

UPDATE institution_roles ir
SET name = src.name,
    description = COALESCE(src.description, ir.description),
    is_active = src.is_active,
    updated_at = NOW()
FROM (
  SELECT
    LOWER(TRIM(code)) AS code,
    COALESCE(NULLIF(TRIM(name_cn), ''), NULLIF(TRIM(code), ''), 'unknown') AS name,
    NULLIF(TRIM(description), '') AS description,
    COALESCE(is_active, true) AS is_active
  FROM inst_roles
  WHERE TRIM(COALESCE(code, '')) <> ''
) src
WHERE LOWER(TRIM(ir.code)) = src.code
  AND ir.deleted_at IS NULL;

COMMIT;
