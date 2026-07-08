BEGIN;

ALTER TABLE badge_devices
  DROP COLUMN IF EXISTS assigned_to_tenant_at;

ALTER TABLE badge_devices
  DROP COLUMN IF EXISTS assigned_to_emp_at;

COMMIT;
