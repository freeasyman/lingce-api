-- 为电子病历大模型质检写入平台默认模型配置。
--
-- 运行时按 function_type=emr_quality_check 查询，不在业务代码中硬编码模型。
-- 当前默认模型为通义千问 Max；租户级配置仍可按现有选择规则覆盖平台默认配置。
--
-- 署名：Codex
-- 时间：2026-09-14

UPDATE public.llm_model_configs
SET is_default = FALSE,
    updated_at = NOW()
WHERE tenant_id = 0
  AND function_type = 'emr_quality_check'
  AND deleted_at IS NULL;

INSERT INTO public.llm_model_configs (
    tenant_id, model_code, function_type, model_name, provider,
    model_params, extra_params, is_default, is_active, description,
    created_by, created_at, updated_at
)
SELECT
    0,
    'qwen-max',
    'emr_quality_check',
    '通义千问 Max',
    'dashscope',
    '{"temperature":0,"max_tokens":12000,"timeout_seconds":120,"response_format":"json"}'::json,
    '{}'::json,
    TRUE,
    TRUE,
    '电子病历大模型质检平台默认模型配置',
    0,
    NOW(),
    NOW()
WHERE NOT EXISTS (
    SELECT 1
    FROM public.llm_model_configs
    WHERE tenant_id = 0
      AND function_type = 'emr_quality_check'
      AND model_code = 'qwen-max'
      AND deleted_at IS NULL
);

UPDATE public.llm_model_configs
SET model_name = '通义千问 Max',
    provider = 'dashscope',
    model_params = '{"temperature":0,"max_tokens":12000,"timeout_seconds":120,"response_format":"json"}'::json,
    is_active = TRUE,
    is_default = TRUE,
    description = '电子病历大模型质检平台默认模型配置',
    updated_at = NOW()
WHERE tenant_id = 0
  AND function_type = 'emr_quality_check'
  AND model_code = 'qwen-max'
  AND deleted_at IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM public.llm_model_configs
        WHERE tenant_id = 0
          AND function_type = 'emr_quality_check'
          AND model_code = 'qwen-max'
          AND provider = 'dashscope'
          AND is_active = TRUE
          AND deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION '电子病历质检模型配置写入失败';
    END IF;
END $$;
