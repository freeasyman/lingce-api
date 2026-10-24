-- 洞察提取模型配置：深度分析任务用千问 Max，不用 flash。
-- resolveLLMModelConfig 候选 ["content_insight","content_topic","chat"] 会先命中 content_insight。
INSERT INTO llm_model_configs
    (tenant_id, function_type, provider, model_code, model_name, is_default, is_active, model_params, extra_params, description)
VALUES
    (0, 'content_insight', 'dashscope', 'qwen3.8-max', '千问 Max', true, true, '{"timeout_seconds": 180}', '{}', '内容获客·洞察提取（深度分析，用 Max，不设 max_tokens 以兼容 JSON Schema 结构化输出）');
