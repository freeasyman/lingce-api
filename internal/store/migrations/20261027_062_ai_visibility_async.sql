-- 内容获客：AI 可见性诊断改异步，补状态与进度字段。
-- 署名：DSH
-- 时间：2026-10-27

ALTER TABLE content_ai_visibility_runs ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'done';
ALTER TABLE content_ai_visibility_runs ADD COLUMN IF NOT EXISTS progress INTEGER NOT NULL DEFAULT 0;
ALTER TABLE content_ai_visibility_runs ADD COLUMN IF NOT EXISTS total INTEGER NOT NULL DEFAULT 0;
COMMENT ON COLUMN content_ai_visibility_runs.status IS 'running / done / failed';
COMMENT ON COLUMN content_ai_visibility_runs.progress IS '已完成引擎数';
COMMENT ON COLUMN content_ai_visibility_runs.total IS '引擎总数';
