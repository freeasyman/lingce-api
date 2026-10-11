-- 为随访任务补充“退回”动作所需的状态和留痕字段。
-- recording_tasks 已有 return_reason 字段，本迁移直接复用它，
-- 只新增退回时间和退回员工，避免重复建业务字段。
-- 退回与取消是两个不同业务动作：退回表示任务不合适、需要重新处理；
-- 取消表示任务不再执行。已完成和已取消任务不会被退回接口修改。
-- 署名：Codex
-- 时间：2026-09-12

ALTER TABLE IF EXISTS recording_tasks
    ADD COLUMN IF NOT EXISTS returned_at TIMESTAMP WITHOUT TIME ZONE,
    ADD COLUMN IF NOT EXISTS returned_by INTEGER;

-- 约束仅允许系统定义的任务状态，保留旧数据兼容性并新增 returned。
ALTER TABLE IF EXISTS recording_tasks
    DROP CONSTRAINT IF EXISTS recording_tasks_status_check;
ALTER TABLE IF EXISTS recording_tasks
    ADD CONSTRAINT recording_tasks_status_check
    CHECK (status IS NULL OR status IN ('pending', 'assigned', 'completed', 'cancelled', 'returned'));

CREATE INDEX IF NOT EXISTS idx_recording_tasks_returned
    ON recording_tasks (tenant_id, status, returned_at DESC);
