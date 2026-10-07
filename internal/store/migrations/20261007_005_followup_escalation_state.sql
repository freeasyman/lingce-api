ALTER TABLE recording_tasks
 ADD COLUMN IF NOT EXISTS escalation_status TEXT,
 ADD COLUMN IF NOT EXISTS escalation_resolution TEXT,
 ADD COLUMN IF NOT EXISTS escalation_resolved_by BIGINT,
 ADD COLUMN IF NOT EXISTS escalation_resolved_at TIMESTAMPTZ;
UPDATE recording_tasks SET escalation_status='pending'
WHERE source_type IN ('follow_up','followup')
AND NULLIF(trim(doctor_escalation),'') IS NOT NULL AND escalation_status IS NULL;
CREATE INDEX IF NOT EXISTS idx_followup_escalation_queue ON recording_tasks(tenant_id, escalation_status)
WHERE source_type IN ('follow_up','followup');
