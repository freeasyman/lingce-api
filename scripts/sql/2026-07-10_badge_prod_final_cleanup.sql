BEGIN;

-- 1) Normalize lifecycle actions from legacy operation_type.
UPDATE badge_device_lifecycle_logs
SET action = operation_type
WHERE COALESCE(NULLIF(TRIM(action), ''), 'unknown') = 'unknown'
  AND COALESCE(NULLIF(TRIM(operation_type), ''), '') <> '';

-- 2) Merge legacy assignment audit into lifecycle logs by device_id + created_at.
UPDATE badge_device_lifecycle_logs l
SET tenant_id = COALESCE(l.tenant_id, a.tenant_id),
    employee_id = COALESCE(l.employee_id, a.employee_id),
    notes = COALESCE(NULLIF(TRIM(l.notes), ''), NULLIF(TRIM(a.remark), '')),
    extra_data = COALESCE(l.extra_data, '{}'::jsonb) ||
      jsonb_build_object(
        'legacy_assignment_id', a.id,
        'legacy_assignment_status', a.status,
        'legacy_assignment_level', a.assignment_level
      )
FROM badge_device_assignments a
WHERE l.device_id = a.device_id
  AND ABS(EXTRACT(EPOCH FROM (l.created_at - a.created_at))) < 1;

-- 3) Drop old badge device status columns and stale duplicate fields.
ALTER TABLE IF EXISTS badge_devices
  DROP COLUMN IF EXISTS current_status,
  DROP COLUMN IF EXISTS lifecycle_status,
  DROP COLUMN IF EXISTS assignment_status,
  DROP COLUMN IF EXISTS last_inspection_result,
  DROP COLUMN IF EXISTS inspection_result,
  DROP COLUMN IF EXISTS last_inspection_scene,
  DROP COLUMN IF EXISTS last_inspection_at,
  DROP COLUMN IF EXISTS device_id,
  DROP COLUMN IF EXISTS model,
  DROP COLUMN IF EXISTS assigned_to_tenant_at,
  DROP COLUMN IF EXISTS assigned_to_emp_at;

-- 4) Drop legacy tables no longer used by runtime.
DROP TABLE IF EXISTS badge_application_tickets;
DROP TABLE IF EXISTS badge_device_assignments;
DROP TABLE IF EXISTS badge_vendor_sync_items;
DROP TABLE IF EXISTS badge_vendor_sync_batches;

COMMIT;
