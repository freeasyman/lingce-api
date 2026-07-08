BEGIN;

UPDATE badge_devices
SET metadata = COALESCE(metadata, '{}'::jsonb) || COALESCE(ext_json, '{}'::jsonb),
    updated_at = NOW()
WHERE ext_json IS NOT NULL
  AND ext_json <> '{}'::jsonb;

ALTER TABLE badge_devices
  DROP COLUMN IF EXISTS ext_json;

COMMIT;
