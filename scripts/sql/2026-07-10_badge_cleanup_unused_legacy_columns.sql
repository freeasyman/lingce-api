ALTER TABLE IF EXISTS badge_devices
  DROP COLUMN IF EXISTS device_id,
  DROP COLUMN IF EXISTS model,
  DROP COLUMN IF EXISTS assigned_to_tenant_at,
  DROP COLUMN IF EXISTS assigned_to_emp_at,
  DROP COLUMN IF EXISTS inspection_result,
  DROP COLUMN IF EXISTS last_inspection_result,
  DROP COLUMN IF EXISTS last_inspection_scene,
  DROP COLUMN IF EXISTS last_inspection_at;

DROP TABLE IF EXISTS badge_vendor_sync_items;
DROP TABLE IF EXISTS badge_vendor_sync_batches;
