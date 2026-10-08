ALTER TABLE recording_tasks
 ADD COLUMN IF NOT EXISTS patient_replied BOOLEAN,
 ADD COLUMN IF NOT EXISTS revisit_attended BOOLEAN,
 ADD COLUMN IF NOT EXISTS issue_resolved BOOLEAN,
 ADD COLUMN IF NOT EXISTS patient_understanding TEXT;
