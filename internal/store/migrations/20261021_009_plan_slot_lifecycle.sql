ALTER TABLE content_plan_slots
    ADD COLUMN IF NOT EXISTS is_manual BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX IF NOT EXISTS idx_content_plan_slots_manual ON content_plan_slots (tenant_id, week_start, is_manual);
