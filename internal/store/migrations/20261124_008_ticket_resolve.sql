-- 工单流程简化：受理后选择处理方式（换新/远程解决/不处理），去掉审核/执行两步。
-- 状态归一：pending 待受理、progress 处理中、waiting 待寄回、completed 已完成、rejected 不处理。
UPDATE badge_tickets SET status = 'progress' WHERE status IN ('approved', 'failed');
UPDATE badge_tickets SET status = 'pending'  WHERE status IN ('submitted', 'reviewing');

ALTER TABLE badge_tickets
    ADD COLUMN IF NOT EXISTS resolution           TEXT,
    ADD COLUMN IF NOT EXISTS resolved_at          TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS customer_notified_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS customer_notified_by BIGINT;

-- 历史数据回填处理方式；已完成的换新工单标记为 swap。
UPDATE badge_tickets
SET resolution = CASE
    WHEN resolution_type IS NOT NULL AND resolution_type <> '' THEN 'swap'
    WHEN status = 'rejected' THEN 'none'
    WHEN status = 'completed' THEN 'remote'
    ELSE resolution
END
WHERE resolution IS NULL;
