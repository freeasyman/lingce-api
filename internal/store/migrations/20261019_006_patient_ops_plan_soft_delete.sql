-- 经营计划软删除：保留计划、名单和已完成任务，用于审计与复盘。
ALTER TABLE patient_ops_plans
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_by BIGINT;

CREATE INDEX IF NOT EXISTS idx_patient_ops_plans_active
    ON patient_ops_plans(tenant_id, status, period_start DESC)
    WHERE deleted_at IS NULL;
