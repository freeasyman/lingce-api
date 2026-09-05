INSERT INTO recording_analysis_prompts (
  code, name, description, category, system_prompt, user_prompt_template,
  output_schema, version, is_active, usage_count, created_by, updated_by, created_at, updated_at
)
SELECT
  'realtime_transcript_correction_v1',
  '实时转写文字纠错',
  '只修正实时 ASR 文字中的错别字、医学词语、标点和断句，不改变原始事实。',
  'analysis',
  $$你是实时医疗转写文字校对助手。

你的任务只有一件事：校对 ASR 识别出的当前一句文字。

只允许：
1. 修正明显的错别字和同音识别错误；
2. 修正常见医学词语的识别错误；
3. 补充或调整标点和断句；
4. 保持原文的事实和表达范围。

绝对禁止：
1. 修改、猜测或补充姓名、数字、日期、时间、剂量、频次和单位；
2. 添加原文没有出现的症状、检查、诊断、药物或医嘱；
3. 做任何临床判断、用药安全判断或诊疗建议；
4. 改写成病历或总结全文。

如果原文无法可靠纠正，原样返回。
只输出一个合法 JSON 对象，不要 Markdown，不要解释文字。$$,
  $$请校对下面这段实时 ASR 文字，并返回纠错后的文字：

原始 ASR 文字：
{{transcript}}$$,
  $$
{
  "type": "object",
  "required": ["corrected_text"],
  "properties": {
    "corrected_text": {"type": "string"}
  }
}$$::jsonb,
  'v1',
  true,
  0,
  0,
  0,
  NOW(),
  NOW()
WHERE NOT EXISTS (
  SELECT 1 FROM recording_analysis_prompts WHERE code = 'realtime_transcript_correction_v1'
);

INSERT INTO llm_model_configs (
  tenant_id, model_code, function_type, model_name, provider,
  model_params, extra_params, is_default, is_active, description,
  created_by, created_at, updated_at
)
SELECT
  0,
  'qwen-plus',
  'realtime_transcript_correction',
  '通义千问 Plus',
  'dashscope',
  '{"temperature":0.0,"max_tokens":500,"timeout_seconds":15}'::json,
  '{}'::json,
  true,
  true,
  '实时转写文字纠错平台默认模型配置',
  0,
  NOW(),
  NOW()
WHERE NOT EXISTS (
  SELECT 1
  FROM llm_model_configs
  WHERE tenant_id = 0
    AND function_type = 'realtime_transcript_correction'
    AND deleted_at IS NULL
);
