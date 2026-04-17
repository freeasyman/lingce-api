-- Badge status simplification to 5-state model
-- pending / ready / in_use / blocked / retired

BEGIN;

-- 1) Drop old constraints if present.
ALTER TABLE badge_devices DROP CONSTRAINT IF EXISTS ck_badge_devices_status;
ALTER TABLE badge_devices DROP CONSTRAINT IF EXISTS ck_badge_devices_current_status;

-- 2) Normalize status and current_status from legacy values.
UPDATE badge_devices
SET status = CASE lower(trim(COALESCE(status, current_status, '')))
  WHEN 'pending' THEN 'pending'
  WHEN 'draft' THEN 'pending'
  WHEN 'ready' THEN 'ready'
  WHEN 'available' THEN 'ready'
  WHEN 'returned' THEN 'ready'
  WHEN 'in_use' THEN 'in_use'
  WHEN 'blocked' THEN 'blocked'
  WHEN 'broken' THEN 'blocked'
  WHEN 'retired' THEN 'retired'
  WHEN 'scrapped' THEN 'retired'
  ELSE 'pending'
END,
current_status = CASE lower(trim(COALESCE(current_status, status, '')))
  WHEN 'pending' THEN 'pending'
  WHEN 'draft' THEN 'pending'
  WHEN 'ready' THEN 'ready'
  WHEN 'available' THEN 'ready'
  WHEN 'returned' THEN 'ready'
  WHEN 'in_use' THEN 'in_use'
  WHEN 'blocked' THEN 'blocked'
  WHEN 'broken' THEN 'blocked'
  WHEN 'retired' THEN 'retired'
  WHEN 'scrapped' THEN 'retired'
  ELSE 'pending'
END,
lifecycle_status = CASE
  WHEN lower(trim(COALESCE(status, current_status, ''))) IN ('pending', 'draft') THEN 'pending_acceptance'
  WHEN lower(trim(COALESCE(status, current_status, ''))) IN ('retired', 'scrapped') THEN 'scrapped'
  ELSE 'active'
END,
assignment_status = CASE
  WHEN employee_id IS NOT NULL THEN 'employee'
  WHEN tenant_id IS NOT NULL THEN 'tenant'
  ELSE 'unassigned'
END,
health_status = CASE
  WHEN lower(trim(COALESCE(status, current_status, ''))) IN ('blocked', 'broken', 'retired', 'scrapped') THEN 'error'
  WHEN lower(trim(COALESCE(health_status, ''))) = 'normal' THEN 'healthy'
  WHEN health_status IS NULL OR trim(health_status) = '' THEN 'unknown'
  WHEN lower(trim(COALESCE(health_status, ''))) NOT IN ('unknown', 'healthy', 'warning', 'error') THEN 'unknown'
  ELSE health_status
END,
updated_at = NOW()
WHERE deleted_at IS NULL;

-- 3) Add new constraints.
ALTER TABLE badge_devices
  ADD CONSTRAINT ck_badge_devices_status
  CHECK (status IN ('pending','ready','in_use','blocked','retired'));

ALTER TABLE badge_devices
  ADD CONSTRAINT ck_badge_devices_current_status
  CHECK (current_status IN ('pending','ready','in_use','blocked','retired'));

ALTER TABLE badge_devices DROP CONSTRAINT IF EXISTS ck_badge_devices_health_status;
ALTER TABLE badge_devices DROP CONSTRAINT IF EXISTS chk_badge_devices_health_status_enum;
ALTER TABLE badge_devices
  ADD CONSTRAINT ck_badge_devices_health_status
  CHECK (health_status IN ('unknown','healthy','warning','error'));

COMMIT;

-- verification
-- SELECT status, current_status, COUNT(*) FROM badge_devices WHERE deleted_at IS NULL GROUP BY 1,2 ORDER BY 1,2;
