-- 质检规则专科维度：specialty 为空 = 通用规则。
-- 注意 emr_quality_published_rule_set 是 definitions 的视图，需同步重建。
ALTER TABLE public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS specialty TEXT;

UPDATE public.emr_quality_rule_definitions
   SET specialty = '中医'
 WHERE rule_code LIKE 'TCM-%' AND (specialty IS NULL OR specialty = '');

DROP VIEW IF EXISTS public.emr_quality_published_rule_set;
CREATE VIEW public.emr_quality_published_rule_set AS
SELECT id, rule_code, version, section_code, name, source_text, product_text,
       standard_points, executor_kind, input_spec, evaluation_spec, action_policy,
       message_templates, status, specialty, created_at, updated_at
FROM public.emr_quality_rule_definitions
WHERE status = 'published';
