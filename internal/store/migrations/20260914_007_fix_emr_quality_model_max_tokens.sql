-- 修复电子病历质检模型参数。
--
-- DashScope qwen-max 当前接受的 max_tokens 范围为 1-8192。
-- 006 迁移已经可能在部分环境执行，因此使用独立迁移修正既有配置，
-- 不能只修改旧迁移文件。
--
-- 署名：Codex
-- 时间：2026-09-14

UPDATE public.llm_model_configs
SET model_params = jsonb_set(
        COALESCE(model_params::jsonb, '{}'::jsonb),
        '{max_tokens}',
        '8192'::jsonb,
        TRUE
    )::json,
    updated_at = NOW()
WHERE tenant_id = 0
  AND function_type = 'emr_quality_check'
  AND model_code = 'qwen-max'
  AND deleted_at IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.llm_model_configs
        WHERE tenant_id = 0
          AND function_type = 'emr_quality_check'
          AND model_code = 'qwen-max'
          AND deleted_at IS NULL
          AND COALESCE((model_params->>'max_tokens')::integer, 0) > 8192
    ) THEN
        RAISE EXCEPTION '电子病历质检模型 max_tokens 仍超过 DashScope 限制';
    END IF;
END $$;
