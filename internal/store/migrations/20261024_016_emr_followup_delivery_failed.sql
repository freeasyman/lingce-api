-- 实时随访投递增加终态 failed。
--
-- 旧实现只有 pending/accepted，录音被删除或引用永久不成立时无法停止重试，
-- 会每 5 分钟无限写入。新增 failed 后，永久错误和重试耗尽可以明确终止。
--
-- 署名：Codex
-- 时间：2026-10-24

-- 旧表的内联 CHECK 约束名由 PostgreSQL 自动生成，不能假设固定名称。
-- 先按定义匹配删除所有 status 检查，再建立允许 failed 的新约束。
DO $$
DECLARE
    constraint_name text;
BEGIN
    FOR constraint_name IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'emr_followup_deliveries'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) ILIKE '%status%'
    LOOP
        EXECUTE format('ALTER TABLE emr_followup_deliveries DROP CONSTRAINT %I', constraint_name);
    END LOOP;
END $$;

ALTER TABLE emr_followup_deliveries
    ADD CONSTRAINT emr_followup_deliveries_status_check
    CHECK (status IN ('pending', 'accepted', 'failed'));

CREATE INDEX IF NOT EXISTS idx_emr_followup_delivery_failed
    ON emr_followup_deliveries(updated_at DESC)
    WHERE status = 'failed';
