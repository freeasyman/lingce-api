-- 旧模型已下架，清理由通用折扣规则推导出的缓存价，避免配置页展示误导。
UPDATE llm_model_configs
SET cached_input_token_price = NULL,
    explicit_cached_input_token_price = NULL,
    cache_creation_token_price = NULL,
    updated_at = NOW()
WHERE model_code IN ('qwen-max', 'qwen-plus', 'qwen3-vl-plus')
  AND deleted_at IS NULL;
