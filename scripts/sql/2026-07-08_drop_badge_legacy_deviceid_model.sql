BEGIN;

ALTER TABLE badge_devices
  DROP COLUMN IF EXISTS device_id;

ALTER TABLE badge_devices
  DROP COLUMN IF EXISTS model;

COMMIT;
