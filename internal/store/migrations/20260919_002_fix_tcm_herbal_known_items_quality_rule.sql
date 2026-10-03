-- 将“中药处方保留已知药材和用法信息”从程序规则修正为大模型规则。
--
-- 该规则需要对照原始转写/生成依据判断“已知药材和用法信息是否被完整
-- 保留”，不是结构化病历字段本身可以确定性判断的格式或非空规则。

UPDATE public.emr_fields f
SET quality_rules = updated.quality_rules,
    updated_at = NOW(),
    updated_by = 0
FROM (
  SELECT
    f.id,
    jsonb_agg(
      CASE
        WHEN rule_item.rule ->> 'code' = 'known_items_complete' THEN
          rule_item.rule
          || jsonb_build_object(
            'execution', 'llm',
            'llm_instruction', '对照原始转写、生成依据和结构化中药处方，判断录音中明确出现的药材、剂量、用法、频次和疗程是否被完整保留；不能根据证候或常见方剂补全未明确出现的内容。'
          )
        ELSE rule_item.rule
      END
      ORDER BY rule_item.ordinality
    ) AS quality_rules
  FROM public.emr_fields f
  CROSS JOIN LATERAL jsonb_array_elements(f.quality_rules) WITH ORDINALITY AS rule_item(rule, ordinality)
  WHERE f.tenant_id = 0
    AND f.code = 'tcm_herbal_prescription'
  GROUP BY f.id
) updated
WHERE f.id = updated.id;

UPDATE public.emr_quality_rules
SET source_version = '2026-09-19-field-centric-tcm-herbal-known-items-llm'
WHERE tenant_id = 0
  AND code = 'field.tcm_herbal_prescription.known_items_complete';

DO $$
DECLARE
  execution_value TEXT;
  instruction_value TEXT;
BEGIN
  SELECT rule_item.rule ->> 'execution',
         rule_item.rule ->> 'llm_instruction'
  INTO execution_value, instruction_value
  FROM public.emr_fields f
  CROSS JOIN LATERAL jsonb_array_elements(f.quality_rules) AS rule_item(rule)
  WHERE f.tenant_id = 0
    AND f.code = 'tcm_herbal_prescription'
    AND rule_item.rule ->> 'code' = 'known_items_complete';

  IF execution_value <> 'llm' OR COALESCE(instruction_value, '') = '' THEN
    RAISE EXCEPTION '中药处方 known_items_complete 规则未正确修正为大模型判断';
  END IF;
END $$;
