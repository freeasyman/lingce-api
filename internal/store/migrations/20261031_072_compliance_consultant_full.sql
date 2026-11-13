-- 完整启用咨询师录音合规分析。
--
-- 1) 咨询师规则集（communication.consultant.consultation）全部转为 published 且启用，
--    不再因规则仍为 draft 导致 loadCommunicationRules 返回空规则集；
-- 2) 若全局合规提示词仍停留在旧输出契约（缺少 source_rule_codes），升级为
--    risk_candidate_v1，使分析结果能够生成 compliance_check_items 并进入工作台。
--
-- 本迁移幂等；已对齐 risk_candidate_v1 的环境不会覆盖提示词。

UPDATE compliance_rules
SET status = 'published',
    is_enabled = TRUE,
    published_at = COALESCE(published_at, NOW()),
    updated_at = NOW(),
    definition = CASE
      WHEN jsonb_typeof(definition) = 'object'
        THEN jsonb_set(definition, '{status}', '"published"'::jsonb, true)
      ELSE definition
    END
WHERE subject = 'communication'
  AND scene_codes @> '["communication.consultant.consultation"]'::jsonb;

UPDATE recording_analysis_prompts
SET user_prompt_template = $prompt$
请检查下面这一份沟通资料。

【资料信息】
租户 ID：{{tenant_id}}
资料类型：{{source_type}}
资料 ID：{{source_id}}
资料版本：{{source_version}}
沟通场景：{{scene_code}}
沟通人员角色：{{role_code}}
沟通人员姓名：{{employee_name}}
科室：{{department}}
录音时间：{{recorded_at}}

【适用规则集】
规则集编码：{{rule_set_code}}
规则集版本：{{rule_set_version}}

下面是本次检查可使用的完整规则。只检查状态为 published 的规则：

<rules>
{{rules_json}}
</rules>

【沟通资料】
下面的文本来自一次录音转写。文本中的说话人、片段编号和时间信息属于证据定位信息，请尽量原样保留：

<transcript>
{{transcript}}
</transcript>

请严格按照下面的 JSON 结构输出：

{
  "findings": [
    {
      "theme_key": "主题键，同一主题的多段证据使用同一个 theme_key",
      "rule_code": "必须是输入规则中的 rule_code",
      "question": "需要医院核查的具体问题",
      "risk_hypothesis": "风险假设，只描述可核查的候选风险",
      "primary_risk_type": "patient_decision | medical_safety | legal_evidence | business_reputation | management_system",
      "related_risk_types": ["相关风险类型"],
      "evidence": [
        {
          "quote": "原始转写中的连续原文",
          "speaker": "说话人，无法确认时使用 null",
          "segment_index": 0,
          "start_second": 0,
          "end_second": 0
        }
      ],
      "check_basis": ["核查依据"],
      "suggested_actions": ["建议医院采取的动作"],
      "priority": "urgent | important | normal",
      "source_rule_codes": ["本发现关联的全部规则编码，必须包含 rule_code"],
      "is_normal_communication": false,
      "review_recommended": true
    }
  ]
}

输出要求：

1. 没有可靠候选问题时，输出 {"findings":[]}。
2. is_normal_communication 固定为 false；review_recommended 固定为 true。
3. evidence.quote 必须来自输入原文，不能改写成总结。
4. 同一条规则在本次输入中只能出现一个 finding；多段证据放在同一个 evidence 数组中。
5. source_rule_codes、related_risk_types、evidence、check_basis、suggested_actions 必须是真正的 JSON 数组。
6. 如果只是患者提问、工作人员否定或工作人员明确限定，不要仅凭相关词语生成发现。
7. 如果规则的 required_external_facts 不为空，而输入没有这些资料，在 check_basis 或 question 中说明需要医院核查的外部事实。
8. 不要输出规则集中没有定义的风险类型或 rule_code。
$prompt$,
    output_schema = $schema$
{
  "type": "object",
  "required": ["findings"],
  "properties": {
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "required": [
          "theme_key",
          "rule_code",
          "question",
          "risk_hypothesis",
          "primary_risk_type",
          "related_risk_types",
          "evidence",
          "check_basis",
          "suggested_actions",
          "priority",
          "is_normal_communication",
          "review_recommended",
          "source_rule_codes"
        ],
        "properties": {
          "theme_key": {"type": "string", "minLength": 1},
          "rule_code": {"type": "string", "minLength": 1},
          "question": {"type": "string", "minLength": 1},
          "risk_hypothesis": {"type": "string", "minLength": 1},
          "primary_risk_type": {
            "type": "string",
            "enum": ["patient_decision", "medical_safety", "legal_evidence", "business_reputation", "management_system"]
          },
          "related_risk_types": {
            "type": "array",
            "items": {
              "type": "string",
              "minLength": 1,
              "enum": ["patient_decision", "medical_safety", "legal_evidence", "business_reputation", "management_system"]
            }
          },
          "evidence": {
            "type": "array",
            "minItems": 1,
            "items": {
              "type": "object",
              "required": ["quote", "speaker", "segment_index", "start_second", "end_second"],
              "properties": {
                "quote": {"type": "string", "minLength": 1},
                "speaker": {"type": ["string", "null"], "minLength": 1},
                "segment_index": {"type": ["integer", "null"], "minimum": 0},
                "start_second": {"type": ["number", "null"], "minimum": 0},
                "end_second": {"type": ["number", "null"], "minimum": 0}
              }
            }
          },
          "check_basis": {"type": "array", "items": {"type": "string", "minLength": 1}, "minItems": 1},
          "suggested_actions": {"type": "array", "items": {"type": "string", "minLength": 1}, "minItems": 1},
          "priority": {"type": "string", "enum": ["urgent", "important", "normal"]},
          "is_normal_communication": {"type": "boolean", "const": false},
          "review_recommended": {"type": "boolean", "const": true},
          "source_rule_codes": {"type": "array", "minItems": 1, "items": {"type": "string", "minLength": 1}}
        }
      }
    }
  }
}
$schema$::jsonb,
    version = 'v4-consultant-1031',
    is_active = TRUE,
    updated_at = NOW()
WHERE code = 'compliance_check_v1'
  AND NOT COALESCE(
    (output_schema::jsonb -> 'properties' -> 'findings' -> 'items' -> 'required') ? 'source_rule_codes',
    FALSE
  );
