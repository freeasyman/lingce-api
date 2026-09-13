-- Seed the canonical outpatient EMR working-draft generation prompt and model.
--
-- Runtime selection:
--   1. recording_analysis_prompts stores the prompt definition.
--   2. llm_model_configs stores the model provider and model code.
--   3. The EMR internal API selects model configuration by
--      function_type = 'emr_candidate_generation', preferring the tenant
--      configuration and falling back to tenant_id = 0.
--
-- This migration is safe to run after the EMR model rebuild migration, which
-- removed the former candidate-generation seed.
--
-- 署名：Codex
-- 时间：2026-09-13

BEGIN;

UPDATE recording_analysis_prompts
SET
  name = '电子病历工作稿生成',
  description = '根据完整医生与患者接诊转写，按照当前门急诊模板生成供医生核对的结构化病历工作稿。',
  category = 'analysis',
  system_prompt = $emr_system$
你是门急诊电子病历文书生成助手。

你的任务是根据完整的医生与患者接诊转写、患者信息、就诊信息和当前病历模板，生成一份结构化的门急诊病历工作稿，供医生核对、修改和确认。

请只记录输入材料中明确出现或可以直接整理得到的事实。不得凭常识补造患者信息、症状、体征、检查结果、诊断、药品、剂量、疗程或治疗结论。
医生明确说出的诊断、处置、医嘱和处方可以整理到对应栏位。患者自己的判断不能自动写成医生诊断。
患者没有回答、医生没有询问、文字无法确认或前后矛盾的内容，不得写成“无”“正常”或其他确定结论；请留空，并在 unresolved_items 中说明原因。
只能使用输入中的模板栏位编码，不得新增或删除模板栏位。working_content 必须包含模板中的全部栏位。
对话中的口语、重复、语病和错别字只做必要的文书整理，不改变原意，不把推测当成事实。
诊断只能整理医生明确表达的诊断，不得自行推断疾病或提出新的医疗建议。
这个任务只生成病历，不做质量检查，不输出质量结论或合格判断。
只输出一个合法 JSON 对象，不要输出 Markdown、解释文字或代码围栏。
$emr_system$,
  user_prompt_template = $emr_user$
请根据以下完整接诊材料，生成一份结构化门急诊病历工作稿。

【输入模式】
{{input_mode}}

【当前模板】
{{template}}

【模板栏位】
模板中的每个栏位都必须出现在 working_content 中。请使用以下栏位编码、中文名称、内容分类、输入形式、栏位说明和结构定义：
{{template_fields}}

【患者信息】
只有明确标记为已确认的信息，才能写入病历：
{{patient_context}}

【就诊信息】
{{encounter_context}}

【完整接诊转写】
以下是本次工牌录音完成后的完整清洗转写，可能包含口语、重复、语病、错别字、说话人识别错误和时间间隔。请只根据其中明确表达的内容生成病历：
{{transcript}}

请严格按照系统要求返回 JSON。
$emr_user$,
  output_schema = $emr_schema$
{
  "type": "object",
  "required": ["working_content", "unresolved_items"],
  "properties": {
    "working_content": {
      "type": "object",
      "description": "必须包含当前模板中的全部栏位编码"
    },
    "unresolved_items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["field_code", "reason"],
        "properties": {
          "field_code": {"type": "string"},
          "reason": {"type": "string"}
        }
      }
    }
  }
}
$emr_schema$::jsonb,
  updated_at = NOW()
WHERE code = 'emr_working_draft_generation_v1';

INSERT INTO recording_analysis_prompts (
  code, name, description, category, system_prompt, user_prompt_template,
  output_schema, version, is_active, usage_count, created_by, updated_by,
  created_at, updated_at
)
SELECT
  'emr_working_draft_generation_v1',
  '电子病历工作稿生成',
  '根据完整医生与患者接诊转写，按照当前门急诊模板生成供医生核对的结构化病历工作稿。',
  'analysis',
  $emr_system$
你是门急诊电子病历文书生成助手。

你的任务是根据完整的医生与患者接诊转写、患者信息、就诊信息和当前病历模板，生成一份结构化的门急诊病历工作稿，供医生核对、修改和确认。

请只记录输入材料中明确出现或可以直接整理得到的事实。不得凭常识补造患者信息、症状、体征、检查结果、诊断、药品、剂量、疗程或治疗结论。
医生明确说出的诊断、处置、医嘱和处方可以整理到对应栏位。患者自己的判断不能自动写成医生诊断。
患者没有回答、医生没有询问、文字无法确认或前后矛盾的内容，不得写成“无”“正常”或其他确定结论；请留空，并在 unresolved_items 中说明原因。
只能使用输入中的模板栏位编码，不得新增或删除模板栏位。working_content 必须包含模板中的全部栏位。
对话中的口语、重复、语病和错别字只做必要的文书整理，不改变原意，不把推测当成事实。
诊断只能整理医生明确表达的诊断，不得自行推断疾病或提出新的医疗建议。
这个任务只生成病历，不做质量检查，不输出质量结论或合格判断。
只输出一个合法 JSON 对象，不要输出 Markdown、解释文字或代码围栏。
$emr_system$,
  $emr_user$
请根据以下完整接诊材料，生成一份结构化门急诊病历工作稿。

【输入模式】
{{input_mode}}

【当前模板】
{{template}}

【模板栏位】
模板中的每个栏位都必须出现在 working_content 中。请使用以下栏位编码、中文名称、内容分类、输入形式、栏位说明和结构定义：
{{template_fields}}

【患者信息】
只有明确标记为已确认的信息，才能写入病历：
{{patient_context}}

【就诊信息】
{{encounter_context}}

【完整接诊转写】
以下是本次工牌录音完成后的完整清洗转写，可能包含口语、重复、语病、错别字、说话人识别错误和时间间隔。请只根据其中明确表达的内容生成病历：
{{transcript}}

请严格按照系统要求返回 JSON。
$emr_user$,
  $emr_schema$
{
  "type": "object",
  "required": ["working_content", "unresolved_items"],
  "properties": {
    "working_content": {
      "type": "object",
      "description": "必须包含当前模板中的全部栏位编码"
    },
    "unresolved_items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["field_code", "reason"],
        "properties": {
          "field_code": {"type": "string"},
          "reason": {"type": "string"}
        }
      }
    }
  }
}
$emr_schema$::jsonb,
  'v1',
  true,
  0,
  0,
  0,
  NOW(),
  NOW()
WHERE NOT EXISTS (
  SELECT 1
  FROM recording_analysis_prompts
  WHERE code = 'emr_working_draft_generation_v1'
);

UPDATE llm_model_configs
SET is_default = false, updated_at = NOW()
WHERE tenant_id = 0
  AND function_type = 'emr_candidate_generation'
  AND deleted_at IS NULL;

UPDATE llm_model_configs
SET
  model_code = 'qwen-max',
  model_name = '通义千问 Max',
  provider = 'dashscope',
  model_params = '{"temperature":0.0,"max_tokens":4000,"timeout_seconds":120}'::json,
  extra_params = '{}'::json,
  is_default = true,
  is_active = true,
  description = '电子病历工作稿生成平台默认模型配置',
  updated_at = NOW()
WHERE tenant_id = 0
  AND function_type = 'emr_candidate_generation'
  AND deleted_at IS NULL
  AND id = (
    SELECT id
    FROM llm_model_configs
    WHERE tenant_id = 0
      AND function_type = 'emr_candidate_generation'
      AND deleted_at IS NULL
    ORDER BY id
    LIMIT 1
  );

INSERT INTO llm_model_configs (
  tenant_id, model_code, function_type, model_name, provider,
  model_params, extra_params, is_default, is_active, description,
  created_by, created_at, updated_at
)
SELECT
  0,
  'qwen-max',
  'emr_candidate_generation',
  '通义千问 Max',
  'dashscope',
  '{"temperature":0.0,"max_tokens":4000,"timeout_seconds":120}'::json,
  '{}'::json,
  true,
  true,
  '电子病历工作稿生成平台默认模型配置',
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

COMMIT;
