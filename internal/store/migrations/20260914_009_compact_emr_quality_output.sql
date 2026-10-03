-- 压缩电子病历大模型质检输出。
--
-- 质检规则数量较多时，模型不需要重复返回规则名称、执行方式、
-- 严重程度等数据库已经保存的信息。模型只返回规则编码和判断结果，
-- API 再根据输入规则补全展示字段，避免 JSON 因输出过长被截断。
--
-- 署名：Codex
-- 时间：2026-09-14

UPDATE public.recording_analysis_prompts
SET
  system_prompt =
    LEFT(system_prompt, POSITION('十五、严格输出要求' IN system_prompt) - 1)
    || $emr_quality_compact_system$
十五、严格输出要求

为了避免输出过长，模型结果必须使用以下精简格式。

1. 每一条输入规则必须返回一条结果；
2. rule_code 必须与输入规则完全一致，不得新增、遗漏或重复；
3. 规则名称、执行方式、质量分组、严重程度和截止动作已经在输入规则中提供，模型不要重复返回；
4. 每条结果必须包含 evidence，evidence 是一句能够回溯到病历字段的简短证据；
5. conclusion 为“符合”或“未触发”时，只填写 rule_code、conclusion、check_status、handling_result、evidence；
6. conclusion 为“不符合”或“无法判断”时，才填写 detail、missing_facts、suggested_handling；
7. evidence、detail、suggested_handling 必须简短，避免复述完整病历；
8. 不得输出规则列表之外的问题；
9. 只输出合法 JSON，不要 Markdown、代码围栏或解释文字。

顶层结构：

{
  "overall_result": "无问题|存在提示|需要处理|无法完成",
  "model_check_summary": "一句话总结",
  "results": [
    {
      "rule_code": "规则编码",
      "conclusion": "符合|不符合|无法判断",
      "check_status": "已完成|未触发",
      "handling_result": "无需处理|需要医生处理|需要人工复核",
      "evidence": "一句简短证据",
      "detail": "问题说明；符合或未触发时为空字符串",
      "missing_facts": ["缺失事实；没有时为空数组"],
      "suggested_handling": "处理建议；符合或未触发时为空字符串"
    }
  ]
}

处理结果必须与结论一致：

- 符合：已完成、无需处理；
- 未触发：未触发、无需处理；
- 不符合：已完成、需要医生处理；
- 无法判断：已完成、需要人工复核。

如果没有输入规则，返回：

{
  "overall_result": "无法完成",
  "model_check_summary": "本次没有提供可执行的大模型质检规则。",
  "results": []
}
$emr_quality_compact_system$,
  user_prompt_template =
    LEFT(user_prompt_template, POSITION('【输出要求】' IN user_prompt_template) - 1)
    || $emr_quality_compact_user$
【输出要求】

请严格按照 system prompt 中的精简 JSON 结构返回。

必须满足：

1. 每一条输入规则返回一条结果；
2. rule_code 必须与输入规则完全一致；
3. 不得新增、遗漏或重复规则；
4. 每条结果必须有一句简短 evidence；
5. 只有“不符合”和“无法判断”才填写 detail、missing_facts、suggested_handling；
6. 只输出合法 JSON。
$emr_quality_compact_user$,
  output_schema = $emr_quality_compact_schema$
{
  "type": "object",
  "required": ["overall_result", "model_check_summary", "results"],
  "additionalProperties": false,
  "properties": {
    "overall_result": {"type": "string", "enum": ["无问题", "存在提示", "需要处理", "无法完成"]},
    "model_check_summary": {"type": "string"},
    "results": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["rule_code", "conclusion", "check_status", "handling_result", "evidence"],
        "additionalProperties": false,
        "properties": {
          "rule_code": {"type": "string"},
          "conclusion": {"type": "string", "enum": ["符合", "不符合", "无法判断"]},
          "check_status": {"type": "string", "enum": ["已完成", "未触发"]},
          "handling_result": {"type": "string", "enum": ["无需处理", "需要医生处理", "需要人工复核"]},
          "evidence": {"type": "string"},
          "detail": {"type": "string"},
          "missing_facts": {"type": "array", "items": {"type": "string"}},
          "suggested_handling": {"type": "string"}
        }
      }
    }
  }
}
$emr_quality_compact_schema$::jsonb,
  version = 'v2',
  updated_at = NOW()
WHERE code = 'emr_quality_check_v1';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM public.recording_analysis_prompts
        WHERE code = 'emr_quality_check_v1'
          AND is_active = TRUE
          AND version = 'v2'
          AND POSITION('精简格式' IN system_prompt) > 0
          AND POSITION('rule_code' IN user_prompt_template) > 0
    ) THEN
        RAISE EXCEPTION '电子病历质检精简输出提示词写入失败';
    END IF;
END $$;
