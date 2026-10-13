-- 随访生命周期直接存储为 to_execute/completed/returned/cancelled。
-- 执行人是否指定以 assigned_to 为唯一事实；两维度不互相覆盖。
ALTER TABLE recording_tasks DROP CONSTRAINT IF EXISTS recording_tasks_status_check;
UPDATE recording_tasks SET status = CASE
    WHEN status IN ('completed','returned','cancelled') THEN status ELSE 'to_execute' END
WHERE source_type IN ('follow_up','followup');
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='recording_tasks_status_check') THEN
        ALTER TABLE recording_tasks ADD CONSTRAINT recording_tasks_status_check CHECK (status IS NULL OR status IN ('pending','assigned','to_execute','completed','cancelled','returned'));
    END IF;
END $$;
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='followup_execution_status_check') THEN
        ALTER TABLE recording_tasks ADD CONSTRAINT followup_execution_status_check CHECK (source_type NOT IN ('follow_up','followup') OR (status IS NOT NULL AND status IN ('to_execute','completed','returned','cancelled')));
    END IF;
END $$;
ALTER TABLE recording_tasks ADD COLUMN IF NOT EXISTS execution_status TEXT GENERATED ALWAYS AS (CASE WHEN source_type IN ('follow_up','followup') THEN status END) STORED;
ALTER TABLE recording_tasks ADD COLUMN IF NOT EXISTS assignment_status TEXT GENERATED ALWAYS AS (CASE WHEN source_type IN ('follow_up','followup') THEN CASE WHEN assigned_to IS NULL THEN 'executor_unassigned' ELSE 'executor_assigned' END END) STORED;
CREATE INDEX idx_followup_execution_queue ON recording_tasks(tenant_id,execution_status,assignment_status,due_at)
    WHERE source_type IN ('follow_up','followup');
