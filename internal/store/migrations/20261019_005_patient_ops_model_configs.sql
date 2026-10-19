-- 患者运营专用模型配置。
-- 规则：业务代码只按 function_type 读取配置并经 llm-gateway 调用；
-- 模型选择由这条配置负责，代码里不判断模型名、不做强制回落。
-- 经营计划客户内容生成使用独立配置，不与通用 chat/其它业务共用。
UPDATE llm_model_configs
SET function_type = 'patient_ops_plan_content',
    description = '患者运营专用｜经营计划客户内容生成（仅经 llm-gateway）',
    updated_at = NOW()
WHERE function_type = 'patient_plan_member_content'
  AND NOT EXISTS (
      SELECT 1 FROM llm_model_configs
      WHERE function_type = 'patient_ops_plan_content' AND deleted_at IS NULL
  );

INSERT INTO llm_model_configs (
    tenant_id, model_code, function_type, model_name, provider,
    model_params, extra_params, is_default, is_active, description,
    created_by, created_at, updated_at
)
SELECT
    0,
    'qwen3.8-flash',
    'patient_ops_plan_content',
    '患者运营-经营计划客户内容生成',
    'dashscope',
    '{"temperature":0.3,"max_tokens":3000,"timeout_seconds":90}'::json,
    '{}',
    true,
    true,
    '患者运营专用｜经营计划客户内容生成（仅经 llm-gateway）',
    0,
    NOW(),
    NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM llm_model_configs
    WHERE function_type = 'patient_ops_plan_content'
      AND is_active = true
      AND deleted_at IS NULL
);

UPDATE llm_model_configs
SET description = '患者运营专用｜经营计划客户内容生成（仅经 llm-gateway）',
    updated_at = NOW()
WHERE function_type = 'patient_ops_plan_content'
  AND deleted_at IS NULL;

INSERT INTO llm_model_configs (
    tenant_id, model_code, function_type, model_name, provider,
    model_params, extra_params, is_default, is_active, description,
    created_by, created_at, updated_at
)
SELECT
    0,
    'qwen3.8-flash',
    'patient_product_keywords',
    '患者运营-产品匹配关键词',
    'dashscope',
    '{"temperature":0.2,"max_tokens":400,"timeout_seconds":30}'::json,
    '{}',
    true,
    true,
    '患者运营专用｜产品匹配关键词建议（仅经 llm-gateway）',
    0,
    NOW(),
    NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM llm_model_configs
    WHERE function_type = 'patient_product_keywords'
      AND is_active = true
      AND deleted_at IS NULL
);

UPDATE llm_model_configs
SET description = '患者运营专用｜产品匹配关键词建议（仅经 llm-gateway）',
    updated_at = NOW()
WHERE function_type = 'patient_product_keywords'
  AND deleted_at IS NULL;
