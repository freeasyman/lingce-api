BEGIN;

ALTER TABLE IF EXISTS badge_devices
  DROP COLUMN IF EXISTS last_online_time,
  DROP COLUMN IF EXISTS remain_power,
  DROP COLUMN IF EXISTS ext_json;

COMMIT;
