-- 下架旧模型：qwen-max、qwen-plus、qwen3-vl-plus。
-- 当前只保留 Qwen3.8-Max / Qwen3.8-Flash 作为在用文本模型。
UPDATE llm_model_configs
SET is_active = false,
    description = '已下架（2026-10），请使用 qwen3.8-max / qwen3.8-flash',
    updated_at = NOW()
WHERE model_code IN ('qwen-max', 'qwen-plus', 'qwen3-vl-plus')
  AND deleted_at IS NULL;
