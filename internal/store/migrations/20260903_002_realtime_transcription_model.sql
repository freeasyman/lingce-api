INSERT INTO llm_model_configs (
  tenant_id, model_code, function_type, model_name, provider,
  model_params, extra_params, is_default, is_active, description,
  created_by, created_at, updated_at
)
SELECT
  0,
  'paraformer-realtime-v2',
  'realtime_transcription',
  'Paraformer 实时转写',
  'dashscope',
  '{"sample_rate":16000,"format":"pcm"}'::json,
  '{}'::json,
  true,
  true,
  '实时麦克风接诊平台默认 ASR 模型配置',
  0,
  NOW(),
  NOW()
WHERE NOT EXISTS (
  SELECT 1
  FROM llm_model_configs
  WHERE tenant_id = 0
    AND function_type = 'realtime_transcription'
    AND deleted_at IS NULL
);
