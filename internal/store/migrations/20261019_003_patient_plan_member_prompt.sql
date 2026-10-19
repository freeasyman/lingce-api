-- 经营计划：按客户生成个性化执行内容（话术、目标、风险、完成标准）。
INSERT INTO recording_analysis_prompts (
    code, name, description, category, system_prompt, user_prompt_template,
    output_schema, version, is_active, usage_count, created_by, updated_by, created_at, updated_at
)
SELECT
    'patient_plan_member_v1',
    '经营计划客户执行内容',
    '基于客户事实、场景和产品，为每位客户生成可执行的沟通目标、动作、话术、风险和完成标准。',
    'generation',
    '你是医疗机构患者运营助手。只依据输入的患者事实、场景和产品资料生成执行建议，不编造诊断、预约、购买、价格或疗效。不确定的内容写“待确认”。只输出 JSON，不要 Markdown。',
    '计划类型：{{plan_type}}
场景：{{scenario}}
产品资料：{{products}}

客户名单（JSON）：
{{customers}}

请为每位客户生成以下字段：
- goal：本次沟通目标（不超过30字）
- action：员工具体动作（不超过30字）
- content：可直接发送给客户的话术（60-150字，引用客户关注点和顾虑，不承诺疗效，不编造价格）
- risk_medical：医疗风险提示（不超过50字，说明需医生评估，不做诊断）
- risk_commercial：商业风险提示（不超过50字）
- expected_result：做到什么算完成（不超过30字）

要求：
1. 只能使用提供的客户事实和产品资料；
2. 同一批客户的话术必须体现各自的具体关注点，不能通用；
3. 不要输出“已预约/已到院/已购买”等未发生事实；
4. 只输出 JSON：{"members":[{"customer_id":1,"goal":"","action":"","content":"","risk_medical":"","risk_commercial":"","expected_result":""}]}',
    '{}'::jsonb,
    'v1',
    true,
    0,
    0,
    0,
    NOW(),
    NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM recording_analysis_prompts WHERE code = 'patient_plan_member_v1'
);

INSERT INTO llm_model_configs (
    tenant_id, model_code, function_type, model_name, provider,
    model_params, extra_params, is_default, is_active, description,
    created_by, created_at, updated_at
)
SELECT
    0,
    'qwen3.8-flash',
    'patient_plan_member_content',
    '经营计划客户执行内容模型',
    'dashscope',
    '{"temperature":0.3,"max_tokens":3000,"timeout_seconds":90}'::json,
    '{}',
    true,
    true,
    '患者运营经营计划客户级内容生成',
    0,
    NOW(),
    NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM llm_model_configs
    WHERE tenant_id = 0
      AND function_type = 'patient_plan_member_content'
      AND deleted_at IS NULL
);
