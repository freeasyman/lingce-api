-- 收敛电子病历大模型质检输出，避免多规则结果超过模型输出上限。
--
-- 模型只返回规则编码、结论、状态、处理结果和短证据；
-- 规则名称、字段名称、严重程度等由 API 根据已加载规则补全。
--
-- 署名：Codex
-- 时间：2026-09-20

UPDATE public.recording_analysis_prompts
SET
  system_prompt =
    LEFT(system_prompt, POSITION('十五、严格输出要求' IN system_prompt) - 1)
    || $emr_quality_compact_v3_system$
十五、严格输出要求

为了保证多条规则能够稳定返回，必须使用精简 JSON。

1. 每一条输入规则必须返回一条结果，不得新增、遗漏或重复。
2. rule_code 必须与输入规则完全一致。
3. 不要返回 rule_name、configured_execution_mode、actual_execution_mode、severity、confidence。
4. 每条结果只允许包含：
   rule_code、conclusion、check_status、handling_result、evidence、detail、missing_facts、suggested_handling。
5. evidence 必须是一个不超过 40 个汉字的短字符串，说明对应病历栏位的支持、缺失、冲突或不足。
6. 不得返回 evidence 数组，不得复述完整病历，不得重复规则定义。
7. 符合或未触发时，detail、missing_facts、suggested_handling 使用空字符串或空数组。
8. 不符合或无法判断时，只填写最短必要说明。
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
      "evidence": "不超过40个汉字的短证据",
      "detail": "问题说明；符合或未触发时为空字符串",
      "missing_facts": [],
      "suggested_handling": "处理建议；符合或未触发时为空字符串"
    }
  ]
}

处理结果必须与结论一致：
- 符合：已完成、无需处理；
- 未触发：未触发、无需处理；
- 不符合：已完成、需要医生处理；
- 无法判断：已完成、需要人工复核。
$emr_quality_compact_v3_system$,
  user_prompt_template =
    LEFT(user_prompt_template, POSITION('【输出要求】' IN user_prompt_template) - 1)
    || $emr_quality_compact_v3_user$
【输出要求】

只输出精简 JSON，不要输出其他文字。

必须满足：
1. results 恰好包含每一条输入规则一次；
2. rule_code 必须与输入规则完全一致；
3. evidence 是不超过 40 个汉字的字符串，不是数组；
4. 只允许返回 rule_code、conclusion、check_status、handling_result、evidence、detail、missing_facts、suggested_handling；
5. 符合时不要填写 detail、missing_facts、suggested_handling；
6. 不得复述规则定义或完整病历。
$emr_quality_compact_v3_user$,
  output_schema = $emr_quality_compact_v3_schema$
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
          "evidence": {"type": "string", "maxLength": 120},
          "detail": {"type": "string"},
          "missing_facts": {"type": "array", "items": {"type": "string"}},
          "suggested_handling": {"type": "string"}
        }
      }
    }
  }
}
$emr_quality_compact_v3_schema$::jsonb,
  version = 'v3',
  updated_at = NOW()
WHERE code = 'emr_quality_check_v1';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM public.recording_analysis_prompts
    WHERE code = 'emr_quality_check_v1'
      AND is_active = TRUE
      AND version = 'v3'
      AND POSITION('不超过 40 个汉字' IN system_prompt) > 0
  ) THEN
    RAISE EXCEPTION '电子病历质检 v3 精简输出提示词写入失败';
  END IF;
END $$;
