-- Compliance events are tenant-owned facts. Hide the pre-MVP global sample
-- records without destroying their audit trail.
UPDATE compliance_events
SET deleted_at = NOW(),
    updated_at = NOW()
WHERE tenant_id = 0
  AND id IN ('evt-001', 'evt-002');
