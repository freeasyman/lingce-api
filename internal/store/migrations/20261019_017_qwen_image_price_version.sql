-- Qwen-Image-3.0 价格版本标记，便于审计和历史重算。
UPDATE llm_model_configs
SET price_version = '2026-10-qwen-image-3.0',
    updated_at = NOW()
WHERE model_code = 'qwen-image-3.0'
  AND deleted_at IS NULL
  AND (price_version IS NULL OR price_version = '');
