UPDATE content_prompt_templates
SET system_prompt = '你是医疗 GEO 内容编辑。只输出合法 JSON；结论前置，条件、边界、风险写清楚；不编造数据，不承诺疗效。',
    prompt_template = '请围绕「{{title}}」写一篇结构化 GEO 文章。\n背景材料：{{context}}\n目标读者：正在比较术式的患者/家属。\n风格：{{style}}。\n只输出 JSON，字段：\n{"title":"","summary":"","takeaways":[""],"conditions":"","notSuitable":"","points":[{"heading":"","body":""}],"terms":[{"term":"","definition":""}],"facts":[{"label":"","value":"","source":""}],"faqs":[{"q":"","a":""}],"related":[{"title":"","url":""}],"disclaimer":""}\n要求：不编造数据；不确定处写“待医生确认”；额外要求：{{additional_requirements}}',
    variables = '["title","context","style","additional_requirements"]'::json,
    status = 'published',
    is_active = true,
    updated_at = NOW()
WHERE code = 'content_geo';
