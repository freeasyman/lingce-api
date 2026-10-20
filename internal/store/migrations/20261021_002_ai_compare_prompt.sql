INSERT INTO content_prompt_templates
  (tenant_id, name, code, description, system_prompt, prompt_template, variables, function_type, version, status, is_active, is_system)
VALUES
  (NULL, 'AI 先答对比', 'ai_assistant_compare', '把同一问题的多环境回答归纳为共同点/差异/缺口/动作',
   '你是医疗内容研究助手。只根据提供的各环境回答做归纳，不补充外部事实；区分共同点、差异、信息缺口，并给出一个内容动作。不诊断、不承诺疗效。',
   '患者问题：{{question}}\n\n各环境回答：\n{{answers}}\n\n请输出 JSON：{"common":"共同点","difference":"差异","gap":"缺口","action":"建议动作"}。只输出 JSON。',
   '["question","answers"]'::json, 'ai_assistant_compare', '1.0', 'published', true, true)
ON CONFLICT (code) DO UPDATE SET
  system_prompt = EXCLUDED.system_prompt,
  prompt_template = EXCLUDED.prompt_template,
  variables = EXCLUDED.variables,
  function_type = EXCLUDED.function_type,
  status = 'published',
  is_active = true,
  updated_at = NOW();

INSERT INTO llm_model_configs
  (tenant_id, provider, model_code, model_name, function_type, is_default, is_active, model_params, description, created_by)
SELECT 0, 'dashscope', 'qwen3.8-max', '通义千问 3.8 Max', 'ai_assistant_compare', true, true, '{"temperature":0.2,"max_tokens":1200}'::json, 'AI 先答对比归纳', 0
WHERE NOT EXISTS (
  SELECT 1 FROM llm_model_configs
  WHERE tenant_id=0 AND function_type='ai_assistant_compare' AND provider='dashscope' AND model_code='qwen3.8-max' AND deleted_at IS NULL
);
