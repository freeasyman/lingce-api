-- AI 口碑诊断与内容，记录来源（灵感 insight / 选题 topic），打通 录音→灵感→选题→诊断→文章 的溯源链
-- 署名：DSH
-- 时间：2026-10-27

ALTER TABLE content_ai_visibility_runs ADD COLUMN IF NOT EXISTS insight_id bigint;
ALTER TABLE content_ai_visibility_runs ADD COLUMN IF NOT EXISTS topic_id bigint;
ALTER TABLE content_items ADD COLUMN IF NOT EXISTS insight_id bigint;

CREATE INDEX IF NOT EXISTS idx_ai_visibility_runs_topic
    ON content_ai_visibility_runs (topic_id) WHERE topic_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_content_items_insight
    ON content_items (insight_id) WHERE insight_id IS NOT NULL;
