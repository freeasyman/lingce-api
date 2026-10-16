ALTER TABLE patient_operation_tasks
    ADD COLUMN IF NOT EXISTS result TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS result_note TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS parent_task_id BIGINT REFERENCES patient_operation_tasks(id);

CREATE INDEX IF NOT EXISTS idx_patient_operation_tasks_parent
    ON patient_operation_tasks(tenant_id, parent_task_id);
