-- 同 rule_code 允许「通用 + 各专科」多行共存：唯一约束纳入 specialty（空视为通用）。
DROP INDEX IF EXISTS emr_quality_rule_definitions_code_version_uq;
CREATE UNIQUE INDEX emr_quality_rule_definitions_code_version_uq
  ON public.emr_quality_rule_definitions (rule_code, version, COALESCE(specialty, ''));

-- 眼科：不启用 CC-005（主诉字数检查）。
INSERT INTO public.emr_quality_rule_definitions
  (rule_code, version, section_code, name, source_text, product_text, standard_points,
   executor_kind, input_spec, evaluation_spec, action_policy, message_templates, status, specialty)
SELECT rule_code, version, section_code, name, source_text, product_text, standard_points,
       executor_kind, input_spec,
       jsonb_set(evaluation_spec, '{disabled}', 'true'),
       action_policy, message_templates, status, '眼科'
FROM public.emr_quality_rule_definitions
WHERE rule_code = 'CC-005' AND status = 'published'
ORDER BY created_at DESC
LIMIT 1;
