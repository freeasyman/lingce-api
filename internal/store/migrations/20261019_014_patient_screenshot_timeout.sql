-- 截图多模态分析耗时可能超过 45 秒；统一提高到 180 秒，避免网关 504。
UPDATE llm_model_configs
SET model_params = jsonb_set(COALESCE(model_params::jsonb, '{}'::jsonb), '{timeout_seconds}', '180'::jsonb, true)::json,
    updated_at = NOW()
WHERE function_type = 'patient_screenshot_analysis'
  AND deleted_at IS NULL;
