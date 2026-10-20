-- DeepSeek's reasoning model returns empty content in the gateway's current
-- chat-completions contract; use the chat alias for the comparison surface.
UPDATE llm_model_configs
SET model_code = 'deepseek-chat',
    model_name = 'DeepSeek Chat',
    updated_at = NOW()
WHERE tenant_id = 0 AND function_type = 'ai_assistant' AND provider = 'deepseek' AND deleted_at IS NULL;
