BEGIN;

WITH role_candidates AS (
  SELECT tenant_id, id, code
  FROM institution_roles
  WHERE deleted_at IS NULL
    AND lower(code) IN (
      'doctor',
      'doctor_assistant',
      'therapist',
      'frontdesk',
      'reception',
      'receptionist',
      'consultant',
      'lingce_sales',
      'customer'
    )
),
route_seed AS (
  SELECT
    tenant_id,
    id AS role_id,
    code AS role_code_snapshot,
    CASE
      WHEN lower(code) IN ('doctor', 'doctor_assistant') THEN 'doctor'
      WHEN lower(code) IN ('therapist') THEN 'therapist'
      WHEN lower(code) IN ('frontdesk', 'reception', 'receptionist') THEN 'frontdesk'
      WHEN lower(code) IN ('consultant') THEN 'consultant'
      WHEN lower(code) IN ('lingce_sales') THEN 'lingce_sales'
      WHEN lower(code) IN ('customer') THEN 'customer'
      ELSE 'unknown'
    END AS scene_scope,
    CASE
      WHEN lower(code) IN ('doctor', 'doctor_assistant') THEN 'doctor'
      WHEN lower(code) IN ('therapist') THEN 'therapist'
      WHEN lower(code) IN ('frontdesk', 'reception', 'receptionist') THEN 'frontdesk'
      WHEN lower(code) IN ('consultant') THEN 'consultant'
      WHEN lower(code) IN ('lingce_sales') THEN 'lingce_sales'
      WHEN lower(code) IN ('customer') THEN 'customer'
      ELSE 'unknown'
    END AS pipeline_code,
    'v1'::VARCHAR(64) AS pipeline_version
  FROM role_candidates
),
seed_clock AS (
  SELECT NOW() AS ts
)
INSERT INTO analysis_role_routes (
  tenant_id,
  role_id,
  role_code_snapshot,
  scene_scope,
  pipeline_code,
  pipeline_version,
  enabled,
  effective_at,
  created_at,
  updated_at
)
SELECT
  rs.tenant_id,
  rs.role_id,
  rs.role_code_snapshot,
  rs.scene_scope,
  rs.pipeline_code,
  rs.pipeline_version,
  TRUE AS enabled,
  sc.ts AS effective_at,
  sc.ts AS created_at,
  sc.ts AS updated_at
FROM route_seed rs
CROSS JOIN seed_clock sc
WHERE rs.scene_scope <> 'unknown'
  AND NOT EXISTS (
    SELECT 1
    FROM analysis_role_routes arr
    WHERE arr.tenant_id = rs.tenant_id
      AND arr.role_id = rs.role_id
      AND arr.scene_scope = rs.scene_scope
      AND arr.pipeline_code = rs.pipeline_code
      AND arr.pipeline_version = rs.pipeline_version
      AND arr.enabled = TRUE
  );

COMMIT;
