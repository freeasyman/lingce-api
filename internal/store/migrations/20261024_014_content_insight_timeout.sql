-- 洞察提取用千问 Max 且转写可能很长（数万字），180s 超时不够，提到 600s。
-- 不设 max_tokens（兼容 JSON Schema 结构化输出）。
UPDATE llm_model_configs
SET model_params = '{"timeout_seconds": 600}'::json,
    updated_at = now()
WHERE function_type = 'content_insight' AND deleted_at IS NULL;
