-- 免费体检/AI 可见性缺口诊断单独指定模型：deepseek-chat（比 content_topic 的 qwen3.8-max 快约 3 倍）
-- 代码按 function_type 顺序解析，此处只提供配置，模型可随时在后台改。
INSERT INTO llm_model_configs (tenant_id, model_code, model_name, provider, function_type, is_default, is_active, created_at, updated_at)
SELECT 0, 'deepseek-chat', 'DeepSeek Chat（缺口诊断）', 'deepseek', 'ai_visibility_gap', true, true, now(), now()
WHERE NOT EXISTS (
  SELECT 1 FROM llm_model_configs WHERE function_type = 'ai_visibility_gap' AND tenant_id = 0 AND deleted_at IS NULL
);
