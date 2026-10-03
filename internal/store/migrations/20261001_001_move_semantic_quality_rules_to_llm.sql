-- 将依赖自由文本语义的规则交给大模型。
--
-- 程序规则只保留稳定的存在性、结构和格式判断：
-- 辅助检查的结果/待补状态和处方项目完整性不能从自由文本可靠判断。

UPDATE public.emr_fields f
SET quality_rules = updated.quality_rules,
    updated_at = NOW(),
    updated_by = 0
FROM (
    SELECT
        f.id,
        jsonb_agg(
            CASE
                WHEN f.code = 'auxiliary_exam'
                     AND rule_item.rule ->> 'code' = 'result_for_completed'
                THEN rule_item.rule
                    || jsonb_build_object(
                        'execution', 'llm',
                        'executor', '',
                        'llm_instruction',
                            '判断病历中明确列出的已完成检查是否记录了对应结果；结果缺失时指出具体检查项目，不把检查名称、计划或常识当作检查结果。'
                    )
                WHEN f.code = 'auxiliary_exam'
                     AND rule_item.rule ->> 'code' = 'pending_explicit'
                THEN rule_item.rule
                    || jsonb_build_object(
                        'execution', 'llm',
                        'executor', '',
                        'llm_instruction',
                            '判断病历中明确列出的未完成检查或待补结果是否被清楚标记；没有明确检查对象时不要推断缺项。'
                    )
                WHEN f.code = 'prescription'
                     AND rule_item.rule ->> 'code' = 'item_completeness'
                THEN rule_item.rule
                    || jsonb_build_object(
                        'execution', 'llm',
                        'executor', '',
                        'llm_instruction',
                            '判断明确开具的每个处方项目是否记录了病历中要求且实际提供的药品名称、规格、剂量、用法、频次、疗程和数量；不得根据自由文本关键词猜测缺失字段。'
                    )
                ELSE rule_item.rule
            END
            ORDER BY rule_item.ordinality
        ) AS quality_rules
    FROM public.emr_fields f
    CROSS JOIN LATERAL jsonb_array_elements(f.quality_rules)
        WITH ORDINALITY AS rule_item(rule, ordinality)
    WHERE f.tenant_id = 0
      AND f.code IN ('auxiliary_exam', 'prescription')
    GROUP BY f.id
) updated
WHERE f.id = updated.id;

UPDATE public.emr_template_quality_rules b
SET execution_mode = '大模型判断'
FROM public.emr_quality_rules q
WHERE b.quality_rule_id = q.id
  AND q.tenant_id = 0
  AND q.code IN (
      'field.auxiliary_exam.result_for_completed',
      'field.auxiliary_exam.pending_explicit',
      'field.prescription.item_completeness',
      'field.tcm_herbal_prescription.known_items_complete'
  );
