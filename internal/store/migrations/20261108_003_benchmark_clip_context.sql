-- 金句需要保留当时的对话上下文与使用场景，避免只展示一句孤立的话。
ALTER TABLE benchmark_clips ADD COLUMN IF NOT EXISTS context_text text NOT NULL DEFAULT '';
ALTER TABLE benchmark_clips ADD COLUMN IF NOT EXISTS use_when text NOT NULL DEFAULT '';
