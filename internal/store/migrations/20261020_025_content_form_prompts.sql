-- Seven content forms share the same DB-owned prompt/model contract.
INSERT INTO content_prompt_templates
  (tenant_id, name, code, description, system_prompt, prompt_template, variables, function_type, version, status, is_active, is_system)
VALUES
  (NULL, '朋友圈文案', 'content_moments', '朋友圈海报 + 短文案', '你是医疗私域内容编辑。文案要短、像真人说话；不承诺疗效，不编造患者故事，结尾给一个轻行动。',
   '主题：{{title}}\n背景：{{context}}\n风格：{{style}}\n额外要求：{{additional_requirements}}\n请输出：1 条朋友圈文案（200 字内），再给 3 个海报标题备选。',
   '["title","context","style","additional_requirements"]'::json, 'content_moments', '1.0', 'published', true, true),
  (NULL, '海报文案', 'content_poster', '海报主标题、副标题、要点、行动', '你是医疗海报文案编辑。标题短、可一眼读完；不承诺疗效，不制造焦虑，不写绝对化用语。',
   '主题：{{title}}\n背景：{{context}}\n风格：{{style}}\n额外要求：{{additional_requirements}}\n请输出 JSON：main_title、subtitle、bullets（3-4 条）、cta、footnote。',
   '["title","context","style","additional_requirements"]'::json, 'content_poster', '1.0', 'published', true, true),
  (NULL, 'FAQ 事实页', 'content_faq', '官网可引用的问答', '你是医疗 FAQ 编辑。先给一句话结论，再给适用条件、不适用、风险与例外、来源与更新时间；不承诺疗效。',
   '患者问题：{{title}}\n背景：{{context}}\n额外要求：{{additional_requirements}}\n请输出：结论、适用条件、不适用/需说明、风险与例外、来源与更新时间。',
   '["title","context","additional_requirements"]'::json, 'content_faq', '1.0', 'published', true, true)
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
SELECT v.tenant_id, v.provider, v.model_code, v.model_name, v.function_type, true, true, v.model_params::json, v.description, 0
FROM (VALUES
  (0, 'dashscope', 'qwen3.8-max', '通义千问 3.8 Max', 'content_moments', '{"temperature":0.6,"max_tokens":2000}', '朋友圈短文案'),
  (0, 'dashscope', 'qwen3.8-max', '通义千问 3.8 Max', 'content_poster', '{"temperature":0.5,"max_tokens":2000}', '海报文案'),
  (0, 'dashscope', 'qwen3.8-max', '通义千问 3.8 Max', 'content_faq', '{"temperature":0.3,"max_tokens":3000}', 'FAQ 事实页')
) AS v(tenant_id, provider, model_code, model_name, function_type, model_params, description)
WHERE NOT EXISTS (
  SELECT 1 FROM llm_model_configs c
  WHERE c.tenant_id = v.tenant_id AND c.function_type = v.function_type
    AND c.provider = v.provider AND c.model_code = v.model_code AND c.deleted_at IS NULL
);
