-- Product recommendations must use the institution catalogue, not invented services.
UPDATE recording_analysis_prompts
SET system_prompt = system_prompt || E'\n推荐产品解释时：只允许引用所提供的机构产品库。产品库为空时，明确告知“当前机构产品库没有可用的在售产品，请先补充产品资料”，不得编造本机构提供的项目。截图没有明确需求时，区分生活兴趣与医疗需求；可说明库内哪些产品尚需确认适用条件，但不得把兴趣推断为疾病或强行推荐。',
updated_at=NOW()
WHERE code='patient_action_generation_v1' AND is_active=true
AND system_prompt NOT LIKE '%只允许引用所提供的机构产品库%';
