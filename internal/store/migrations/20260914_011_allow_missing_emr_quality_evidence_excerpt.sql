-- 允许电子病历质检对空栏位使用空证据摘录。
--
-- 缺失或不足本身可能就是质检结论：栏位没有填写时不存在可引用的
-- 原文值。此时必须保留真实 field_code、evidence_type 和 explanation，
-- 但 field_value_excerpt 允许为空字符串，不能因此把有效的质检结果
-- 当成接口解析失败。
--
-- 署名：Codex
-- 时间：2026-09-14

UPDATE public.recording_analysis_prompts
SET
  system_prompt =
    LEFT(system_prompt, POSITION('十五、严格输出要求' IN system_prompt) - 1)
    || $emr_quality_v4_system$
十五、严格输出要求

模型必须对每一条输入的大模型判断规则返回一条结果。rule_code 必须与输入规则完全一致，不得新增、遗漏或重复。

规则名称、执行方式、质量分组、严重程度和截止动作已经在输入规则中提供，模型不要重复返回。

每条结果必须包含 evidence 数组。evidence 中的 field_code 必须是当前病历模板中的栏位 code，不能填写规则编码、自然语言栏位名称或空字符串。field_value_excerpt 必须是该栏位当前值的简短原文摘录。

如果证据类型是“缺失”或“不足”，且该栏位当前没有可引用的文字值，field_value_excerpt 必须填写空字符串；不得为了填充字段而编造原文。此时 explanation 仍必须说明缺失或不足的具体原因。

explanation 必须简短说明该栏位如何支持、违反或不足以判断本条规则。证据类型只能使用“支持”“缺失”“冲突”或“不足”。

符合或未触发时，只填写 rule_code、conclusion、check_status、handling_result 和 evidence；detail、missing_facts、suggested_handling 输出空字符串或空数组。

不符合或无法判断时，除上述字段外，填写简短的 detail、missing_facts 和 suggested_handling。不得复述完整病历。

处理结果必须与结论一致：

- 符合：check_status 为“已完成”，handling_result 为“无需处理”；
- 未触发：check_status 为“未触发”，handling_result 为“无需处理”；
- 不符合：check_status 为“已完成”，handling_result 为“需要医生处理”；
- 无法判断：check_status 为“已完成”，handling_result 为“需要人工复核”。

只输出合法 JSON，不要输出 Markdown、代码围栏或解释文字。

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
      "evidence": [
        {
          "field_code": "当前模板栏位编码",
          "field_value_excerpt": "当前栏位值的简短摘录；缺失或不足且无值时为空字符串",
          "evidence_type": "支持|缺失|冲突|不足",
          "explanation": "该栏位与规则的关系"
        }
      ],
      "detail": "问题说明；符合或未触发时为空字符串",
      "missing_facts": ["缺失事实；没有时为空数组"],
      "suggested_handling": "处理建议；符合或未触发时为空字符串"
    }
  ]
}

如果没有输入规则，返回：

{
  "overall_result": "无法完成",
  "model_check_summary": "本次没有提供可执行的大模型质检规则。",
  "results": []
}
$emr_quality_v4_system$,
  user_prompt_template =
    LEFT(user_prompt_template, POSITION('【输出要求】' IN user_prompt_template) - 1)
    || $emr_quality_v4_user$
【输出要求】

严格按照 system prompt 中的 JSON 结构返回。

每条输入规则返回一条结果，rule_code 必须完全一致，不得新增、遗漏或重复。

每条结果的 evidence 必须是数组，至少包含一个证据对象；每个证据对象必须填写当前模板中真实存在的 field_code、evidence_type 和 explanation。field_value_excerpt 在栏位有值时填写原文摘录；栏位缺失或不足且没有原文值时填写空字符串，不得编造。

只在“不符合”和“无法判断”时填写 detail、missing_facts、suggested_handling；符合和未触发时分别使用“已完成/无需处理”和“未触发/无需处理”。

只输出合法 JSON。
$emr_quality_v4_user$,
  output_schema = $emr_quality_v4_schema$
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
          "evidence": {
            "type": "array",
            "minItems": 1,
            "items": {
              "type": "object",
              "required": ["field_code", "field_value_excerpt", "evidence_type", "explanation"],
              "additionalProperties": false,
              "properties": {
                "field_code": {"type": "string"},
                "field_value_excerpt": {"type": "string"},
                "evidence_type": {"type": "string", "enum": ["支持", "缺失", "冲突", "不足"]},
                "explanation": {"type": "string"}
              }
            }
          },
          "detail": {"type": "string"},
          "missing_facts": {"type": "array", "items": {"type": "string"}},
          "suggested_handling": {"type": "string"}
        }
      }
    }
  }
}
$emr_quality_v4_schema$::jsonb,
  version = 'v4',
  updated_at = NOW()
WHERE code = 'emr_quality_check_v1';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM public.recording_analysis_prompts
        WHERE code = 'emr_quality_check_v1'
          AND is_active = TRUE
          AND version = 'v4'
          AND POSITION('field_value_excerpt 必须填写空字符串' IN system_prompt) > 0
          AND output_schema->'properties'->'results'->'items'->'properties'->'evidence'->>'type' = 'array'
    ) THEN
        RAISE EXCEPTION '电子病历质检 v4 提示词写入失败';
    END IF;
END $$;
