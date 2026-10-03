-- 体温允许只记录数值，不把摄氏度符号作为医生提交门槛。
UPDATE public.emr_fields
SET
    description = REPLACE(description, '结构化生命体征必须保留数值和单位。', '结构化生命体征应记录有效数值；单位不作为体温提交门槛。'),
    quality_rules = COALESCE((
        SELECT jsonb_agg(
            CASE
                WHEN rule_item->>'code' = 'vital_signs_format' THEN
                    rule_item
                    || jsonb_build_object(
                        'name', '生命体征数值有效',
                        'parameters', jsonb_build_object(
                            'check_numeric_value', true,
                            'check_unit', false
                        )
                    )
                ELSE rule_item
            END
            ORDER BY ordinality
        )
        FROM jsonb_array_elements(COALESCE(emr_fields.quality_rules, '[]'::jsonb))
            WITH ORDINALITY AS rules(rule_item, ordinality)
    ), '[]'::jsonb)
WHERE code = 'physical_exam';
