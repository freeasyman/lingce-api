-- 一般项目只检查姓名、性别、出生年月。
-- 民族、婚姻状况、职业、工作单位、住址不再进入质检、评分或完成病历阻断。
UPDATE public.emr_quality_rule_definitions
SET evaluation_spec = jsonb_set(evaluation_spec, '{required_fields}', '["name", "gender", "birth_date"]'::jsonb, true) - 'prompt_fields',
    updated_at = NOW()
WHERE version = '2026-10-04-v2'
  AND rule_code = 'GEN-001';
