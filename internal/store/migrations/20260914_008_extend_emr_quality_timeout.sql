-- 延长电子病历大模型质检的专用调用超时。
--
-- 质检需要逐条处理大量模板绑定规则，120 秒不足以覆盖完整返回。
-- 该配置只影响 function_type=emr_quality_check，不改变病历生成、
-- 合规检查或其他业务的模型超时。
--
-- 署名：Codex
-- 时间：2026-09-14

UPDATE public.llm_model_configs
SET model_params = jsonb_set(
        COALESCE(model_params::jsonb, '{}'::jsonb),
        '{timeout_seconds}',
        '300'::jsonb,
        TRUE
    )::json,
    updated_at = NOW()
WHERE tenant_id = 0
  AND function_type = 'emr_quality_check'
  AND deleted_at IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM public.llm_model_configs
        WHERE tenant_id = 0
          AND function_type = 'emr_quality_check'
          AND deleted_at IS NULL
          AND COALESCE((model_params->>'timeout_seconds')::integer, 0) <> 300
    ) THEN
        RAISE EXCEPTION '电子病历质检模型 timeout_seconds 未正确设置为 300';
    END IF;
END $$;
