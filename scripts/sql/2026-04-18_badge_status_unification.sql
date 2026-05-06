-- Badge status unification (v2 canonical statuses only)
-- Run against lince_medical database.

BEGIN;

-- 1) Drop old constraint first (it doesn't allow v2 values like broken/returned).
ALTER TABLE badge_devices
  DROP CONSTRAINT IF EXISTS ck_badge_devices_status;

-- 2) Backfill current_status from canonical status.
UPDATE badge_devices
SET current_status = CASE LOWER(COALESCE(status, ''))
  WHEN 'draft' THEN 'draft'
  WHEN 'available' THEN 'available'
  WHEN 'in_use' THEN 'in_use'
  WHEN 'returned' THEN 'returned'
  WHEN 'broken' THEN 'broken'
  WHEN 'scrapped' THEN 'scrapped'
  ELSE 'draft'
END
WHERE deleted_at IS NULL;

-- 3) Tighten check constraint on current_status (remove legacy values).
ALTER TABLE badge_devices
  ADD CONSTRAINT ck_badge_devices_status
  CHECK (
    current_status IN (
      'draft',
      'available',
      'in_use',
      'returned',
      'broken',
      'scrapped'
    )
  );

COMMIT;
