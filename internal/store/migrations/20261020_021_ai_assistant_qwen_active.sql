-- AI 先答 uses three independently configured environments; keep Qwen active
-- alongside DeepSeek and Doubao. Model choice remains DB-owned.
UPDATE llm_model_configs
SET is_active = true, updated_at = NOW()
WHERE tenant_id = 0 AND function_type = 'ai_assistant' AND provider = 'dashscope' AND model_code = 'qwen3.8-max' AND deleted_at IS NULL;
