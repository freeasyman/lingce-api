BEGIN;

UPDATE badge_devices
SET status = CASE
  WHEN status = 'pending' THEN 'pending_acceptance'
  WHEN status = 'ready' THEN 'in_stock'
  WHEN status = 'in_use' THEN 'assigned'
  WHEN status = 'blocked' THEN 'returned'
  ELSE status
END,
updated_at = NOW()
WHERE deleted_at IS NULL
  AND status IN ('pending', 'ready', 'in_use', 'blocked');

COMMIT;
