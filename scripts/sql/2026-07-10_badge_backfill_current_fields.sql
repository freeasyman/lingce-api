BEGIN;

-- 1) Status correction by current binding facts.
UPDATE badge_devices
SET status = 'assigned',
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND tenant_id IS NOT NULL
  AND employee_id IS NOT NULL
  AND status <> 'assigned';

UPDATE badge_devices
SET status = 'in_stock',
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND tenant_id IS NULL
  AND employee_id IS NULL
  AND accepted_at IS NOT NULL
  AND status = 'assigned';

-- 2) Display-field backfill from current tenant / employee truth.
UPDATE badge_devices bd
SET tenant_name = t.name,
    updated_at = NOW()
FROM tenants t
WHERE bd.deleted_at IS NULL
  AND bd.tenant_id = t.id
  AND COALESCE(bd.tenant_name, '') IS DISTINCT FROM COALESCE(t.name, '');

UPDATE badge_devices bd
SET employee_name = COALESCE(NULLIF(TRIM(e.name), ''), NULLIF(TRIM(e.full_name), ''), bd.employee_name),
    employee_phone = COALESCE(NULLIF(TRIM(e.phone), ''), bd.employee_phone),
    updated_at = NOW()
FROM employees e
WHERE bd.deleted_at IS NULL
  AND bd.employee_id = e.id
  AND (
    COALESCE(bd.employee_name, '') IS DISTINCT FROM COALESCE(COALESCE(NULLIF(TRIM(e.name), ''), NULLIF(TRIM(e.full_name), '')), '')
    OR COALESCE(bd.employee_phone, '') IS DISTINCT FROM COALESCE(NULLIF(TRIM(e.phone), ''), '')
  );

-- 3) accepted_at backfill from explicit acceptance events.
WITH first_accept AS (
  SELECT device_id, MIN(created_at) AS accepted_at
  FROM badge_device_lifecycle_logs
  WHERE action = 'acceptance_check'
  GROUP BY device_id
)
UPDATE badge_devices bd
SET accepted_at = fa.accepted_at,
    updated_at = NOW()
FROM first_accept fa
WHERE bd.id = fa.device_id
  AND bd.deleted_at IS NULL
  AND bd.accepted_at IS NULL
  AND bd.status IN ('in_stock', 'assigned');

-- 4) assigned_at backfill.
-- Prefer explicit assign logs; if history never wrote assign events, fall back to accepted_at
-- for already assigned devices so the current module has a usable first-known assigned time.
WITH first_assign AS (
  SELECT device_id, MIN(created_at) AS assigned_at
  FROM badge_device_logs
  WHERE operation = 'assign'
  GROUP BY device_id
),
first_assign_lifecycle AS (
  SELECT device_id, MIN(created_at) AS assigned_at
  FROM badge_device_lifecycle_logs
  WHERE action IN ('assign', 'assign_employee', 'assign_tenant')
  GROUP BY device_id
)
UPDATE badge_devices bd
SET assigned_at = COALESCE(fa.assigned_at, fal.assigned_at, bd.accepted_at),
    updated_at = NOW()
FROM first_assign fa
FULL OUTER JOIN first_assign_lifecycle fal ON fa.device_id = fal.device_id
WHERE bd.id = COALESCE(fa.device_id, fal.device_id)
  AND bd.deleted_at IS NULL
  AND bd.status = 'assigned'
  AND bd.assigned_at IS NULL;

UPDATE badge_devices
SET assigned_at = accepted_at,
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND status = 'assigned'
  AND assigned_at IS NULL
  AND accepted_at IS NOT NULL;

COMMIT;
