-- 修复：内容获客 prompt 插入时未填 updated_at（该列无默认值），
-- 导致「录音提示词管理」列表扫描 NULL 时间戳 500。
UPDATE recording_analysis_prompts
SET updated_at = COALESCE(created_at, now())
WHERE updated_at IS NULL;
