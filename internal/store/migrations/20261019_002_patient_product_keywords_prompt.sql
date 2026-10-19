-- 产品匹配关键词建议：提示词与模型配置进入既有提示词体系。
INSERT INTO recording_analysis_prompts (
    code, name, description, category, system_prompt, user_prompt_template,
    output_schema, version, is_active, usage_count, created_by, updated_by, created_at, updated_at
)
SELECT
    'patient_product_keywords_v1',
    '产品匹配关键词建议',
    '根据产品名称、适用条件和卖点提取用于客户匹配的关键词。',
    'generation',
    '你是医疗机构产品运营助手。只根据给出的产品资料提取关键词，不要编造适应症、疗效或价格。只输出 JSON，不要 Markdown。',
    '产品名称：{{name}}
适用条件：{{applicable}}
卖点：{{selling_point}}

请提取 3-8 个用于匹配客户的关键词或短语。要求：
1. 优先使用适用条件里出现的疾病、症状、项目、人群特征；
2. 每个关键词 2-8 个字，不要写整句话；
3. 不要输出价格、疗效承诺、品牌名；
4. 只输出 JSON：{"keywords":["关键词1","关键词2"]}',
    '{}'::jsonb,
    'v1',
    true,
    0,
    0,
    0,
    NOW(),
    NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM recording_analysis_prompts WHERE code = 'patient_product_keywords_v1'
);

INSERT INTO llm_model_configs (
    tenant_id, model_code, function_type, model_name, provider,
    model_params, extra_params, is_default, is_active, description,
    created_by, created_at, updated_at
)
SELECT
    0,
    'qwen3.8-flash',
    'patient_product_keywords',
    '产品匹配关键词建议模型',
    'dashscope',
    '{"temperature":0.2,"max_tokens":400,"timeout_seconds":30}'::json,
    '{}',
    true,
    true,
    '患者运营产品匹配关键词建议',
    0,
    NOW(),
    NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM llm_model_configs
    WHERE tenant_id = 0
      AND function_type = 'patient_product_keywords'
      AND deleted_at IS NULL
);
