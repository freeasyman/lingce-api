-- Content center real loop: external analysis deliveries, DB-owned prompts, and model routing.
-- This migration only touches content-center owned tables and the shared LLM configuration table.

ALTER TABLE content_prompt_templates
  ADD COLUMN IF NOT EXISTS system_prompt TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS content_analysis_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id BIGINT NOT NULL,
    source_system VARCHAR(128) NOT NULL,
    source_record_id VARCHAR(255) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'accepted',
    resource_id VARCHAR(128),
    error_reason TEXT,
    attempts INTEGER NOT NULL DEFAULT 0,
    accepted INTEGER NOT NULL DEFAULT 0,
    source_updated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, source_system, source_record_id)
);
CREATE INDEX IF NOT EXISTS idx_content_analysis_deliveries_tenant_status
  ON content_analysis_deliveries (tenant_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_analysis_deliveries_recording
  ON content_analysis_deliveries (tenant_id, (payload->'data'->>'recording_id'));

-- Prompts are owned by this table. Business code only renders variables; it must not carry fallback prompt text.
UPDATE content_prompt_templates
SET system_prompt = '你是医疗健康内容编辑。只写可核验的科普，不诊断、不承诺疗效、不编造数据；结论前置，条件、风险、例外写清楚。',
    status = 'published',
    is_active = true,
    updated_at = NOW()
WHERE code = 'content_article';

UPDATE content_prompt_templates
SET system_prompt = '你是医生口播稿编辑。用短句、口语、可拍摄的画面写；不承诺疗效，不把个案写成普遍结论。',
    status = 'published',
    is_active = true,
    updated_at = NOW()
WHERE code = 'content_script';

UPDATE content_prompt_templates
SET system_prompt = '你是小红书医疗科普图文编辑。输出严格 JSON；短句、可收藏、有边界；不承诺疗效，不编造患者故事。',
    status = 'published',
    is_active = true,
    updated_at = NOW()
WHERE code = 'content_graphic_note';

UPDATE content_prompt_templates
SET system_prompt = '你是医疗内容策划。只从给定真实材料中提取选题，不猜患者没问过的问题；每个选题必须保留来源证据。',
    status = 'published',
    is_active = true,
    updated_at = NOW()
WHERE code = 'content_topic';

INSERT INTO content_prompt_templates
  (tenant_id, name, code, description, system_prompt, prompt_template, variables, function_type, version, status, is_active, is_system)
VALUES
  (NULL, 'GEO 文章生成', 'content_geo', '官网可被 AI 引用的结构化文章',
   '你是医疗 GEO 内容编辑。文章必须结论前置、分点、给适用/不适用、关键事实与来源、医生与机构信息、更新/复核日期、免责声明；不编造数据，不承诺疗效。',
   '请围绕「{{title}}」写一篇结构化 GEO 文章。\n背景材料：{{context}}\n目标读者：正在比较术式的患者/家属。\n风格：{{style}}。\n必须包含：一句话结论、文章速览、适用条件、不适用/需说明、分点解释、关键术语、关键事实与来源、常见问题、相关阅读、医生与机构信息、免责声明。\n额外要求：{{additional_requirements}}',
   '["title","context","style","additional_requirements"]'::json, 'content_geo', '1.0', 'published', true, true),
  (NULL, '内容通用生成', 'content_generation', '未指定内容类型时的结构化中文内容',
   '你是医疗健康内容编辑。只输出可核验的科普内容，不诊断、不承诺疗效、不编造数据。',
   '标题：{{title}}\n背景：{{context}}\n风格：{{style}}\n目标字数：{{length}}\n额外要求：{{additional_requirements}}\n请输出结构清晰的中文内容。',
   '["title","context","style","length","additional_requirements"]'::json, 'content_generation', '1.0', 'published', true, true),
  (NULL, '段落改写', 'content_rewrite', '编辑选中段落的局部改写',
   '你是医疗内容编辑。只改写用户选中的段落，保持事实和边界不变，不新增数据，不改变结论。',
   '原文：\n{{selected_text}}\n\n上下文：\n{{context}}\n\n改写要求：{{instruction}}\n请只输出改写后的段落。',
   '["selected_text","context","instruction"]'::json, 'content_rewrite', '1.0', 'published', true, true),
  (NULL, '图文页数修复', 'content_graphic_note_repair', '只修复图文卡片数量，不改事实',
   '你是结构化输出修复器。只修复 JSON 结构，不新增事实、不改变结论。',
   '上一版输出的正文卡片数不符合要求。请保持事实、标题、标签不变，只调整 slides，使正文卡片数严格等于 {{target_count}} 张（不含封面）。只输出 JSON。',
   '["target_count"]'::json, 'content_graphic_note_repair', '1.0', 'published', true, true)
ON CONFLICT (code) DO UPDATE SET
  system_prompt = EXCLUDED.system_prompt,
  prompt_template = EXCLUDED.prompt_template,
  variables = EXCLUDED.variables,
  function_type = EXCLUDED.function_type,
  status = 'published',
  is_active = true,
  updated_at = NOW();

-- Model routing is configuration, not code. Every content/AI function resolves through these rows.
INSERT INTO llm_model_configs
  (tenant_id, provider, model_code, model_name, function_type, is_default, is_active, model_params, description, created_by)
SELECT v.tenant_id, v.provider, v.model_code, v.model_name, v.function_type, true, true, v.model_params::json, v.description, 0
FROM (VALUES
  (0, 'dashscope', 'qwen3.8-max', '通义千问 3.8 Max', 'content_article', '{"temperature":0.4,"max_tokens":6000}', '公众号/官网长文'),
  (0, 'dashscope', 'qwen3.8-max', '通义千问 3.8 Max', 'content_graphic_note', '{"temperature":0.5,"max_tokens":5000}', '小红书图文 JSON'),
  (0, 'dashscope', 'qwen3.8-max', '通义千问 3.8 Max', 'content_script', '{"temperature":0.5,"max_tokens":4000}', '医生口播脚本'),
  (0, 'dashscope', 'qwen3.8-max', '通义千问 3.8 Max', 'content_generation', '{"temperature":0.5,"max_tokens":5000}', '通用内容生成'),
  (0, 'dashscope', 'qwen3.8-max', '通义千问 3.8 Max', 'content_geo', '{"temperature":0.3,"max_tokens":7000}', 'GEO 结构化文章'),
  (0, 'dashscope', 'qwen3.8-flash', '通义千问 3.8 Flash', 'content_rewrite', '{"temperature":0.3,"max_tokens":2000}', '局部改写'),
  (0, 'dashscope', 'qwen3.8-max', '通义千问 3.8 Max', 'ai_assistant', '{"temperature":0.3,"max_tokens":1200}', 'AI 先答：千问环境'),
  (0, 'deepseek', 'deepseek-v4-pro', 'DeepSeek V4 Pro', 'ai_assistant', '{"temperature":0.3,"max_tokens":1200}', 'AI 先答：DeepSeek 环境'),
  (0, 'doubao', 'doubao-seed-2-1-pro-260915', '豆包 Seed 2.1 Pro', 'ai_assistant', '{"temperature":0.3,"max_tokens":1200}', 'AI 先答：豆包环境（需在方舟开通模型）')
) AS v(tenant_id, provider, model_code, model_name, function_type, model_params, description)
WHERE NOT EXISTS (
  SELECT 1 FROM llm_model_configs c
  WHERE c.tenant_id = v.tenant_id AND c.function_type = v.function_type
    AND c.provider = v.provider AND c.model_code = v.model_code AND c.deleted_at IS NULL
);
