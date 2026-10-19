-- 经营计划客户内容：补称呼、客户类型和上下文注入规则。
-- 每位客户输入里已包含 salutation、customer_type、age、age_known、gender、
-- platform、nickname、source、decision_maker、interest、concern、clinical_text。
UPDATE recording_analysis_prompts
SET system_prompt = system_prompt || E'\n生成 content 时必须以该客户的 salutation 开头；salutation 为“您好”时直接以“您好”开头。不得自行编造姓名、性别、年龄、家长、先生或女士。',
    updated_at = NOW()
WHERE code = 'patient_plan_member_v1'
  AND system_prompt NOT LIKE '%必须以该客户的 salutation 开头%';

UPDATE recording_analysis_prompts
SET user_prompt_template = user_prompt_template || E'\n\n称呼与上下文规则：\n1. 每位客户的 salutation 是唯一允许使用的称呼；content 必须以它开头。\n2. salutation 为“家长”时，不要写具体孩子姓名；salutation 为“您好”时，不要写先生/女士/家长。\n3. customer_type=未知 或 age_known=false 时，不要推断客户是儿童或成人。\n4. 只能使用输入中的 customer_type、age、gender、platform、nickname、source、decision_maker、interest、concern、clinical_text，不要补造关系人、诊断或购买事实。\n5. 同一批客户的话术必须体现各自称呼和事实，不能通用。',
    updated_at = NOW()
WHERE code = 'patient_plan_member_v1'
  AND user_prompt_template NOT LIKE '%称呼与上下文规则%';
