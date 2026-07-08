BEGIN;

ALTER TABLE badge_devices
  DROP COLUMN IF EXISTS remain_power;

ALTER TABLE badge_devices
  DROP COLUMN IF EXISTS last_online_time;

COMMIT;
