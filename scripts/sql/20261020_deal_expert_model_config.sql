-- 赢客专家分析专用模型配置（tenant 0 全局，租户可在后台覆盖）。
-- 主模型：qwen3.8-max（判断密集任务，与合规/病历质检对齐）。
-- 对照模型：qwen3.8-flash（仅用于回填粗跑与 prompt 迭代 A/B，不设为默认）。
INSERT INTO llm_model_configs (
  tenant_id, model_code, model_name, provider, function_type, is_default, is_active,
  model_params, extra_params, input_token_price, output_token_price, price_currency, description, created_at, updated_at
)
SELECT 0, 'qwen3.8-max', '通义千问 3.8 Max', 'dashscope', 'deal_expert_analysis', TRUE, TRUE,
       '{"temperature":0.1,"max_tokens":8192,"response_format":"json","timeout_seconds":180}'::json,
       '{}'::json, 0.0120, 0.0360, 'CNY', '赢客专家接待分析主模型', NOW(), NOW()
WHERE NOT EXISTS (
  SELECT 1 FROM llm_model_configs
  WHERE tenant_id = 0 AND function_type = 'deal_expert_analysis'
    AND model_code = 'qwen3.8-max' AND deleted_at IS NULL
);

INSERT INTO llm_model_configs (
  tenant_id, model_code, model_name, provider, function_type, is_default, is_active,
  model_params, extra_params, input_token_price, output_token_price, price_currency, description, created_at, updated_at
)
SELECT 0, 'qwen3.8-flash', '通义千问 3.8 Flash', 'dashscope', 'deal_expert_analysis', FALSE, TRUE,
       '{"temperature":0.1,"max_tokens":8192,"response_format":"json","timeout_seconds":180}'::json,
       '{}'::json, 0.0008, 0.0027, 'CNY', '赢客专家分析对照模型：仅回填/A-B，不设为默认', NOW(), NOW()
WHERE NOT EXISTS (
  SELECT 1 FROM llm_model_configs
  WHERE tenant_id = 0 AND function_type = 'deal_expert_analysis'
    AND model_code = 'qwen3.8-flash' AND deleted_at IS NULL
);
