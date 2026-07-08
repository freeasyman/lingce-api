BEGIN;

ALTER TABLE badge_devices
  DROP CONSTRAINT IF EXISTS ck_badge_devices_status;

ALTER TABLE badge_devices
  DROP CONSTRAINT IF EXISTS ck_badge_devices_current_status;

ALTER TABLE badge_devices
  DROP CONSTRAINT IF EXISTS ck_badge_devices_health_status;

ALTER TABLE badge_devices
  DROP CONSTRAINT IF EXISTS chk_badge_devices_health_status_enum;

UPDATE badge_devices
SET status = CASE lower(trim(COALESCE(status, '')))
  WHEN 'pending' THEN 'pending_acceptance'
  WHEN 'draft' THEN 'pending_acceptance'
  WHEN 'ready' THEN CASE
    WHEN employee_id IS NULL AND tenant_id IS NULL AND accepted_at IS NULL THEN 'pending_acceptance'
    ELSE 'in_stock'
  END
  WHEN 'available' THEN 'in_stock'
  WHEN 'returned' THEN 'returned'
  WHEN 'in_use' THEN 'assigned'
  WHEN 'blocked' THEN 'returned'
  WHEN 'broken' THEN 'returned'
  WHEN 'retired' THEN 'retired'
  WHEN 'scrapped' THEN 'retired'
  ELSE 'pending_acceptance'
END,
accepted_at = CASE
  WHEN lower(trim(COALESCE(status, ''))) IN ('ready', 'available', 'returned', 'in_use', 'blocked', 'broken', 'retired', 'scrapped')
       AND accepted_at IS NULL
  THEN updated_at
  ELSE accepted_at
END,
updated_at = NOW()
WHERE deleted_at IS NULL;

ALTER TABLE badge_devices
  ADD CONSTRAINT ck_badge_devices_status
  CHECK (status IN (
    'pending_acceptance',
    'in_stock',
    'assigned',
    'returned',
    'retired'
  ));

ALTER TABLE badge_devices
  DROP COLUMN IF EXISTS current_status;

ALTER TABLE badge_devices
  DROP COLUMN IF EXISTS lifecycle_status;

ALTER TABLE badge_devices
  DROP COLUMN IF EXISTS assignment_status;

CREATE INDEX IF NOT EXISTS idx_badge_devices_status
  ON badge_devices(status);

CREATE INDEX IF NOT EXISTS idx_badge_devices_tenant_status
  ON badge_devices(tenant_id, status)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_badge_devices_employee_status
  ON badge_devices(employee_id, status)
  WHERE deleted_at IS NULL;

COMMIT;
