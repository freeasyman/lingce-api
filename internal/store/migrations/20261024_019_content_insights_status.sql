-- 灵感「处理状态」：待处理(new) / 已忽略(dismissed)；已采纳由选题 evidence_ref 反查派生。
--
-- 背景：正式运行后灵感会非常多，运营记不住哪个看过/哪个还没决定。加 status 字段，
-- 让灵感页变成「收件箱」：默认只看待处理，已忽略沉底，已采纳由选题反查。
--
-- 署名：DSH
-- 时间：2026-10-24

ALTER TABLE content_insights
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'new';

CREATE INDEX IF NOT EXISTS idx_content_insights_status
    ON content_insights (tenant_id, status);
