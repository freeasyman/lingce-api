-- Seed the compliance-check prompt and the platform model default.
-- This is shared platform infrastructure and should be reusable by the worker
-- compliance engine without hardcoding prompt or model choices.

INSERT INTO recording_analysis_prompts (
  code, name, description, category, system_prompt, user_prompt_template,
  output_schema, version, is_active, usage_count, created_by, updated_by, created_at, updated_at
)
SELECT
  'compliance_check_v1',
  '合规卫士检查',
  '根据合规卫士规则集，对录音、病历和内容中的候选问题进行结构化发现输出。',
  'analysis',
  $$你是合规卫士的候选发现助手。你的任务是根据给定的规则集，对输入材料做结构化检查，只输出值得医院复核的候选发现。

要求：
1. 只输出 JSON，不要 Markdown，不要解释文字；
2. 不自动确认违规、不定责、不处罚；
3. 每个发现必须对应具体 rule_code，并附带可核查的原文证据；
4. 同一规则在同一输入中出现多处证据时，合并到同一发现里；
5. 没有候选发现时，findings 为空数组；
6. 只使用规则集中提供的规则编码，不要编造新规则。
$$,
  $$请根据以下输入和规则集输出候选发现：

tenant_id: {{tenant_id}}
scene_code: {{scene_code}}
source_type: {{source_type}}
source_id: {{source_id}}
source_version: {{source_version}}
rule_set_code: {{rule_set_code}}
rule_set_version: {{rule_set_version}}

输入材料：
{{input_text}}
$$,
  $$
{
  "type": "object",
  "required": ["findings"],
  "properties": {
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["rule_code", "found", "summary", "reason", "evidence"],
        "properties": {
          "rule_code": {"type": "string"},
          "found": {"type": "boolean"},
          "summary": {"type": "string"},
          "reason": {"type": "string"},
          "evidence": {
            "type": "array",
            "items": {
              "type": "object",
              "required": ["quote"],
              "properties": {
                "quote": {"type": "string"},
                "segment_index": {"type": "integer"},
                "speaker": {"type": "string"},
                "start_second": {"type": "number"},
                "end_second": {"type": "number"}
              }
            }
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
  SELECT 1 FROM recording_analysis_prompts WHERE code = 'compliance_check_v1'
);

INSERT INTO llm_model_configs (
  tenant_id, model_code, function_type, model_name, provider,
  model_params, extra_params, is_default, is_active, description,
  created_by, created_at, updated_at
)
SELECT
  0,
  'qwen-max',
  'compliance_check',
  '通义千问 Max',
  'dashscope',
  '{"temperature":0.0,"max_tokens":4000,"timeout_seconds":120}'::json,
  '{}'::json,
  true,
  true,
  '合规卫士平台默认模型配置',
  0,
  NOW(),
  NOW()
WHERE NOT EXISTS (
  SELECT 1
  FROM llm_model_configs
  WHERE tenant_id = 0
    AND function_type = 'compliance_check'
    AND deleted_at IS NULL
);
