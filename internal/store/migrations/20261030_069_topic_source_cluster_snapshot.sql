-- 选题来源快照：选题若来自「高频问题」，存当时的高频问题名 + 被问次数 + 高频问题 id，
-- 用于选题列表显示「来自高频问题：XX · 被问 N 次」，且不依赖高频问题表的 id 稳定性。

ALTER TABLE content_topics ADD COLUMN IF NOT EXISTS source_cluster_id bigint;
ALTER TABLE content_topics ADD COLUMN IF NOT EXISTS source_cluster_question text;
ALTER TABLE content_topics ADD COLUMN IF NOT EXISTS source_cluster_frequency int;

COMMENT ON COLUMN content_topics.source_cluster_id IS '来源高频问题 id（跳转参考，可能因重建失效）';
COMMENT ON COLUMN content_topics.source_cluster_question IS '来源高频问题名快照';
COMMENT ON COLUMN content_topics.source_cluster_frequency IS '来源高频问题被问次数快照';
