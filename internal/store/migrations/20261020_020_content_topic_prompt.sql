-- Topic generation prompt must live in the prompt table, not in Go code.
INSERT INTO content_prompt_templates
  (tenant_id, name, code, description, system_prompt, prompt_template, variables, function_type, version, status, is_active, is_system)
VALUES
  (NULL, '真实选题生成', 'content_topic_real', '从真实背景生成选题，不猜患者没问过的问题',
   '你是医疗内容策划。只从给定真实材料中提取选题，不猜患者没问过的问题；每个选题要有明确标题、描述和来源边界。',
   '根据以下真实背景，生成 {{count}} 个内容选题：\n\n背景：{{context}}\n\n要求：\n1. 每个选题有明确标题和描述；\n2. 不编造患者原话或数据；\n3. 返回 JSON 数组，字段为 title、description。\n\n只返回 JSON，不要其他内容。',
   '["count","context"]'::json, 'content_topic', '1.0', 'published', true, true)
ON CONFLICT (code) DO UPDATE SET
  system_prompt = EXCLUDED.system_prompt,
  prompt_template = EXCLUDED.prompt_template,
  variables = EXCLUDED.variables,
  function_type = EXCLUDED.function_type,
  status = 'published',
  is_active = true,
  updated_at = NOW();
