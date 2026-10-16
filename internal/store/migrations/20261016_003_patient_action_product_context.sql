-- Extend the immutable patient action prompt with product-library context.
UPDATE recording_analysis_prompts
SET user_prompt_template = replace(
  user_prompt_template,
  '{{analysis}}',
  '{{analysis}}\n\n产品库可用信息（仅用于推荐产品解释）：\n{{product_context}}'
),
    updated_at = NOW()
WHERE code = 'patient_action_generation_v1'
  AND is_active = true
  AND user_prompt_template NOT LIKE '%{{product_context}}%';
