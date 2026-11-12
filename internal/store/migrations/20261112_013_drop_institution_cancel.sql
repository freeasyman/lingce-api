-- 删除「取消服务」功能遗留的租户列与部分索引。
--
-- 业务方确认不需要取消服务，机构服务状态机回到 5 态：
--   unopened / trial / active / paused / expired
-- 迁移可重复执行：索引与列都用 IF EXISTS 删除，已删过时是 no-op。
-- 注意：不改 20261112_012（已提交迁移只增不删），本迁移只做收尾清理。

DROP INDEX IF EXISTS tenants_cancelled_idx;

ALTER TABLE IF EXISTS tenants
    DROP COLUMN IF EXISTS cancelled_at,
    DROP COLUMN IF EXISTS cancelled_reason;
