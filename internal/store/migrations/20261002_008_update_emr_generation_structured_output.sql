-- 让运行时电子病历生成提示词输出结构化栏位对象。
-- 普通栏位仍输出字符串；input_type=结构化内容的栏位按 structure_json 输出对象。
-- 兼容此前将所有栏位限制为字符串的历史提示词。

BEGIN;

UPDATE public.recording_analysis_prompts
SET
  system_prompt = regexp_replace(
    CASE
      WHEN system_prompt ILIKE '%结构化栏位必须返回 JSON 对象%' THEN system_prompt
      ELSE system_prompt || E'\n\n【结构化栏位输出规则】\n对于 input_type 为“结构化内容”的栏位，返回 JSON 对象，键只能使用结构定义中的子字段；无法确认的子项使用空字符串。普通文本栏位返回字符串。不要把结构化栏位整体拼成一段字符串。'
    END,
    E'(?m)^.*working_content.*每个.*字符串.*$',
    'working_content 中普通栏位值必须是字符串；结构化栏位值必须是对象，按结构定义返回。',
    'g'
  ),
  user_prompt_template = regexp_replace(
    CASE
      WHEN user_prompt_template ILIKE '%physical_exam 应按照结构定义返回%' THEN user_prompt_template
      ELSE user_prompt_template || E'\n\n【结构化栏位输出规则】\n结构化栏位必须返回对象。例如 physical_exam 应按照结构定义返回 temperature、blood_pressure、pulse、respiration、blood_glucose、height、weight、bmi 以及文本子项；没有明确内容的子项使用空字符串。不要把结构化栏位整体拼成一段字符串。'
    END,
    E'(?m)^.*每个 working_content 的值都必须是字符串.*$',
    '普通栏位值必须是字符串；结构化栏位值必须是对象，按结构定义返回。',
    'g'
  ),
  output_schema = (jsonb_set(
    jsonb_set(
      jsonb_set(
        COALESCE(output_schema::jsonb, '{}'::jsonb),
        '{properties,working_content,description}',
        '"按照模板栏位编码保存内容；普通栏位为字符串，结构化栏位为对象。"'::jsonb,
        true
      ),
      '{properties,working_content,additionalProperties}',
      '{"anyOf":[{"type":"string"},{"type":"object"}]}'::jsonb,
      true
    ),
    '{properties,unresolved_items,items,required}',
    '["field_code", "reason"]'::jsonb,
    true
  ))::json,
  updated_at = NOW()
WHERE code = 'emr_working_draft_generation_v1'
  AND is_active = TRUE;

COMMIT;
