-- 程序规则先确定前置事实后，大模型不再重复判断同一事实。
-- 只配置当前已经存在的程序规则，不为缺少程序规则的栏位虚构依赖。

WITH updated AS (
    SELECT
        f.id,
        jsonb_agg(
            CASE
                WHEN r.rule->>'execution' = 'llm'
                 AND f.code = 'chief_complaint'
                 AND r.rule->>'code' IN (
                     'duration',
                     'not_diagnosis_only',
                     'not_vague',
                     'present_illness_consistency',
                     'symptom_sign'
                 )
                THEN jsonb_set(
                    r.rule,
                    '{skip_if}',
                    '[{"field":"chief_complaint","rule":"required","conclusion":"不符合"}]'::jsonb,
                    true
                )
                WHEN r.rule->>'execution' = 'llm'
                 AND f.code = 'diagnosis'
                 AND r.rule->>'code' IN (
                     'certainty_preserved',
                     'disposition_consistency',
                     'evidence_consistency',
                     'not_patient_statement',
                     'prescription_advice_consistency'
                 )
                THEN jsonb_set(
                    r.rule,
                    '{skip_if}',
                    '[{"field":"diagnosis","rule":"present","conclusion":"不符合"}]'::jsonb,
                    true
                )
                WHEN r.rule->>'execution' = 'llm'
                 AND f.code = 'allergy_history'
                 AND r.rule->>'code' = 'prescription_consistency'
                THEN jsonb_set(
                    r.rule,
                    '{skip_if}',
                    '[{"field":"allergy_history","rule":"status_valid","conclusion":"不符合"},{"field":"allergy_history","rule":"unconfirmed","conclusion":"不符合"}]'::jsonb,
                    true
                )
                ELSE r.rule
            END
            ORDER BY r.ordinality
        ) AS quality_rules
    FROM public.emr_fields f
    CROSS JOIN LATERAL jsonb_array_elements(COALESCE(f.quality_rules, '[]'::jsonb)) WITH ORDINALITY AS r(rule, ordinality)
    WHERE f.tenant_id = 0
    GROUP BY f.id
)
UPDATE public.emr_fields f
SET quality_rules = updated.quality_rules,
    updated_at = NOW()
FROM updated
WHERE f.id = updated.id;
