-- 决定卡可以派生多条任务（1-3 条），原先“每接待仅一条活跃任务”的约束过严。
-- 改成：同一接待 + 同一任务目标唯一，避免重复生成，同时允许一单多任务。
DROP INDEX IF EXISTS deal_follow_ups_one_active_per_encounter;

CREATE UNIQUE INDEX IF NOT EXISTS deal_follow_ups_one_active_task_per_recording
ON deal_follow_ups (tenant_id, recording_id, objective)
WHERE status = ANY (ARRAY['待处理'::text, '处理中'::text, '等待客户'::text, '等待内部协作'::text, '暂停联系'::text]);
