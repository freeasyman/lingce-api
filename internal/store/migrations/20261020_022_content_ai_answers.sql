-- AI 先答: persisted environment answers + DB-owned answer prompt.
CREATE TABLE IF NOT EXISTS content_ai_answers (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    question TEXT NOT NULL,
    provider VARCHAR(50) NOT NULL,
    model_code VARCHAR(100) NOT NULL,
    model_name VARCHAR(200),
    content TEXT,
    error_reason TEXT,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_content_ai_answers_tenant_question
  ON content_ai_answers (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_ai_answers_question
  ON content_ai_answers (tenant_id, question, created_at DESC);

INSERT INTO content_prompt_templates
  (tenant_id, name, code, description, system_prompt, prompt_template, variables, function_type, version, status, is_active, is_system)
VALUES
  (NULL, 'AI 先答回答', 'ai_assistant_answer', '同一患者问题在多个模型环境下的回答',
   '你是医疗内容研究助手。只回答患者真实问过的问题；结论前置，区分已知事实、行业共识和不确定信息；不诊断、不承诺疗效、不编造数据；需要时写明“具体以医生面诊为准”。',
   '患者问题：{{question}}\n\n请直接回答这个问题。要求：\n1. 先给一句话结论；\n2. 再给 2-4 个要点；\n3. 写清适用条件、风险或不确定处；\n4. 不写“作为AI”之类的套话。',
   '["question"]'::json, 'ai_assistant', '1.0', 'published', true, true)
ON CONFLICT (code) DO UPDATE SET
  system_prompt = EXCLUDED.system_prompt,
  prompt_template = EXCLUDED.prompt_template,
  variables = EXCLUDED.variables,
  function_type = EXCLUDED.function_type,
  status = 'published',
  is_active = true,
  updated_at = NOW();

-- Repair prompt is now fully table-owned; include the generated JSON variable.
UPDATE content_prompt_templates
SET prompt_template = '上一版输出的正文卡片数不符合要求。请保持事实、标题、标签不变，只调整 slides，使正文卡片数严格等于 {{target_count}} 张（不含封面）。只输出 JSON。\n\n原始 JSON：\n{{generated_json}}',
    variables = '["target_count","generated_json"]'::json,
    updated_at = NOW()
WHERE code = 'content_graphic_note_repair';
