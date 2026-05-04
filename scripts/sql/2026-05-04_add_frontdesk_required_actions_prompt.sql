-- Add P_FRONTDESK_REQUIRED_ACTIONS prompt for generating improvement actions
-- from aggregated key_issues and risk_compliance data.
-- This replaces the required_actions portion of the old P_FRONTDESK_SHIFT_SYNTHESIZE.

INSERT INTO recording_analysis_prompts (
    code, name, description, category,
    system_prompt, user_prompt_template, output_schema,
    version, is_active, created_by, updated_by, usage_count,
    created_at, updated_at
) VALUES (
    'P_FRONTDESK_REQUIRED_ACTIONS',
    '前台待办动作生成',
    '基于聚合后的质检问题和风险项，生成录音级别的改进待办动作',
    'frontdesk_analysis',
    '你是医院前台服务质量改进顾问。
根据提供的质检问题和风险项，提炼出可执行的改进动作。

硬约束：
1. 动作数量自然产出，0-5条，不凑数。
2. 每条必须有：action（具体动作）、owner（责任人角色）、deadline（合理期限）、acceptance_criteria（验收标准）。
3. 动作必须可执行、可验收，不写空泛建议。
4. 如果质检问题和风险项都很轻微或为空，可以输出空数组。
5. 优先级：高风险 > 不符合 > 部分符合 > 漏答。',
    '输入：
- recording_id: {{recording_id}}
- key_issues: {{key_issues}}
- risk_compliance: {{risk_compliance}}
- closure_events: {{closure_events}}

请根据以上质检结果，生成改进待办动作列表，输出 JSON：
{
  "required_actions": [
    {
      "action": "具体改进动作描述",
      "owner": "责任人角色（如：前台主管、培训部、前台员工）",
      "deadline": "合理期限（如：立即、本周内、下次培训前）",
      "acceptance_criteria": "验收标准"
    }
  ]
}

要求：
1. 动作要针对具体问题，不要泛泛而谈。
2. 同类问题合并为一条动作。
3. 如果没有需要改进的问题，输出空数组 {"required_actions": []}。',
    '{"type":"object","required":["required_actions"],"properties":{"required_actions":{"type":"array","items":{"type":"object","required":["action","owner","deadline","acceptance_criteria"],"properties":{"action":{"type":"string"},"owner":{"type":"string"},"deadline":{"type":"string"},"acceptance_criteria":{"type":"string"}}}}}}',
    '1.0',
    true,
    1, 1, 0,
    NOW(), NOW()
) ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    system_prompt = EXCLUDED.system_prompt,
    user_prompt_template = EXCLUDED.user_prompt_template,
    output_schema = EXCLUDED.output_schema,
    version = EXCLUDED.version,
    is_active = EXCLUDED.is_active,
    updated_at = NOW();
