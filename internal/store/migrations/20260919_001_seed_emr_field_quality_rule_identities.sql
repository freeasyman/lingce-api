-- 为栏位中心化质量规则建立正式结果表可引用的稳定规则身份。
--
-- 当前 emr_check_results.quality_requirement_id 外键指向 emr_quality_rules(id)。
-- 栏位规则存储在 emr_fields.quality_rules JSONB 中，本迁移只为这些规则
-- 生成稳定的 emr_quality_rules 记录，不改变质检结果表结构。

INSERT INTO public.emr_quality_rules
  (tenant_id, code, name, quality_group, rule_type, evaluated_fact, pass_condition,
   precondition, evidence_basis, source_name, source_version, status, created_by,
   published_at, published_by)
SELECT
  0 AS tenant_id,
  'field.' || f.code || '.' || (rule_item.rule ->> 'code') AS code,
  COALESCE(NULLIF(rule_item.rule ->> 'name', ''), f.name || '质量规则') AS name,
  '栏位中心化质量规则' AS quality_group,
  CASE
    WHEN rule_item.rule ->> 'code' IN ('required', 'present') THEN '缺项'
    ELSE '风险提醒'
  END AS rule_type,
  f.name || '：' || COALESCE(NULLIF(rule_item.rule ->> 'name', ''), rule_item.rule ->> 'code') AS evaluated_fact,
  COALESCE(NULLIF(rule_item.rule ->> 'name', ''), '满足栏位质量规则') AS pass_condition,
  CASE
    WHEN COALESCE(tf_required.required_count, 0) > 0 THEN '模板中该栏位必填'
    ELSE '模板中该栏位非必填或条件触发'
  END AS precondition,
  COALESCE(NULLIF(f.description, ''), f.name) AS evidence_basis,
  'emr_fields.quality_rules' AS source_name,
  '2026-09-19-field-centric' AS source_version,
  'published' AS status,
  0 AS created_by,
  NOW() AS published_at,
  0 AS published_by
FROM public.emr_fields f
CROSS JOIN LATERAL jsonb_array_elements(f.quality_rules) AS rule_item(rule)
LEFT JOIN (
  SELECT field_id, COUNT(*) FILTER (WHERE is_required) AS required_count
  FROM public.emr_template_fields
  GROUP BY field_id
) tf_required ON tf_required.field_id = f.id
WHERE f.tenant_id = 0
  AND COALESCE((rule_item.rule ->> 'enabled')::boolean, false) = true
  AND rule_item.rule ? 'code'
ON CONFLICT (tenant_id, code) DO UPDATE
SET name = EXCLUDED.name,
    quality_group = EXCLUDED.quality_group,
    rule_type = EXCLUDED.rule_type,
    evaluated_fact = EXCLUDED.evaluated_fact,
    pass_condition = EXCLUDED.pass_condition,
    precondition = EXCLUDED.precondition,
    evidence_basis = EXCLUDED.evidence_basis,
    source_name = EXCLUDED.source_name,
    source_version = EXCLUDED.source_version,
    status = 'published',
    published_at = COALESCE(public.emr_quality_rules.published_at, NOW()),
    published_by = COALESCE(public.emr_quality_rules.published_by, 0),
    disabled_at = NULL,
    disabled_by = NULL;

DO $$
DECLARE
  missing_count INTEGER;
BEGIN
  SELECT COUNT(*)
  INTO missing_count
  FROM public.emr_fields f
  CROSS JOIN LATERAL jsonb_array_elements(f.quality_rules) AS rule_item(rule)
  LEFT JOIN public.emr_quality_rules q
    ON q.tenant_id = 0
   AND q.code = 'field.' || f.code || '.' || (rule_item.rule ->> 'code')
  WHERE f.tenant_id = 0
    AND COALESCE((rule_item.rule ->> 'enabled')::boolean, false) = true
    AND q.id IS NULL;

  IF missing_count > 0 THEN
    RAISE EXCEPTION '栏位质量规则身份缺失 % 条', missing_count;
  END IF;
END $$;
