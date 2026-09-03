-- Seed the EMR candidate-generation prompt and its platform model default.
-- The recording and LLM configuration tables are shared platform infrastructure.

INSERT INTO recording_analysis_prompts (
  code, name, description, category, system_prompt, user_prompt_template,
  output_schema, version, is_active, usage_count, created_by, updated_by, created_at, updated_at
)
SELECT
  'emr_candidate_generation_v1',
  '电子病历候选生成',
  '根据已完成的医生接诊转写，按门急诊病历栏目生成待医生核对的候选内容。',
  'analysis',
  $$你是电子病历文书整理助手。你只能根据提供的医生与患者接诊转写，按病历栏目整理“待医生核对”的候选内容。

要求：
1. 只记录转写中明确出现的事实或医生明确表达的内容，不得补造姓名、数值、检查结果、诊断、药物、剂量或治疗结论；
2. 诊断、处方、处置、医嘱等内容只能整理对话中已经明确说出的内容，不得提出新的医疗建议；
3. 没有可靠内容的栏目不要输出；
4. section_code 只能使用给定的病历栏目编码；
5. source_evidence 必须引用支持该候选的转写原文片段；
6. 只输出一个合法 JSON 对象，不要 Markdown 或解释文字。

这是文书整理，不是临床决策、用药安全检查或诊疗建议。$$,
  $$请根据以下接诊转写生成病历候选：

录音 ID：{{recording_id}}
分析管线：{{pipeline_code}}

允许的栏目编码：
chief_complaint、present_illness、past_history、personal_history、family_history、allergy_history、physical_exam、auxiliary_exam、diagnosis、prescription、disposition、medical_advice、followup、supplement

接诊转写：
{{transcript}}$$,
  $$
{
  "type": "object",
  "required": ["candidates"],
  "properties": {
    "candidates": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["section_code", "content", "source_evidence"],
        "properties": {
          "section_code": {"type": "string"},
          "content": {"type": "object"},
          "source_evidence": {
            "type": "object",
            "required": ["transcript_excerpt"],
            "properties": {"transcript_excerpt": {"type": "string"}}
          }
        }
      }
    }
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
  SELECT 1 FROM recording_analysis_prompts WHERE code = 'emr_candidate_generation_v1'
);

INSERT INTO llm_model_configs (
  tenant_id, model_code, function_type, model_name, provider,
  model_params, extra_params, is_default, is_active, description,
  created_by, created_at, updated_at
)
SELECT
  0,
  'qwen-plus',
  'emr_candidate_generation',
  '通义千问 Plus',
  'dashscope',
  '{"temperature":0.1,"max_tokens":4000,"timeout_seconds":120}'::json,
  '{}'::json,
  true,
  true,
  '电子病历候选生成平台默认模型配置',
  0,
  NOW(),
  NOW()
WHERE NOT EXISTS (
  SELECT 1
  FROM llm_model_configs
  WHERE tenant_id = 0
    AND function_type = 'emr_candidate_generation'
    AND deleted_at IS NULL
);
