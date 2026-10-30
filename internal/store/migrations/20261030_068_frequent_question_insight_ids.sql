-- 高频问题（反复问）表补存 insight_ids：稳定关联到底层洞察，
-- 让「高频问题 → 选题」能精准溯源，不再靠 topic 字符串反查。

ALTER TABLE content_question_clusters ADD COLUMN IF NOT EXISTS insight_ids jsonb;

COMMENT ON COLUMN content_question_clusters.insight_ids IS '支撑该高频问题的洞察 id 数组（稳定溯源通道）';
