-- 补齐经营计划客户内容提示词：年龄未知时不要推断为儿童/家长/成人。
UPDATE recording_analysis_prompts
SET system_prompt = system_prompt || E'\n客户 age_known=false 时，不要假设客户是儿童、家长或成人，不要写“孩子/家长”等年龄推断，使用中性称呼；产品名称中的“儿童”只表示产品定位，不代表客户年龄。',
    updated_at = NOW()
WHERE code = 'patient_plan_member_v1'
  AND is_active = true
  AND system_prompt NOT LIKE '%age_known=false%';
