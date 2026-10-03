-- +goose Up

UPDATE emr_fields AS target
SET quality_rules = (
    SELECT jsonb_agg(
        CASE
            WHEN field_rule->>'code' = 'date_format' AND field.code = 'followup'
                THEN jsonb_set(
                    jsonb_set(field_rule, '{parameters}', '{"date_format":"YYYY-MM-DD","allow_trailing_note":true}'::jsonb),
                    '{llm_instruction}',
                    '"复诊安排必须包含真实有效的日期；日期后可以补充说明，但说明不是必填。"'::jsonb
                )
            WHEN field_rule->>'code' = 'valid_date' AND field.code = 'last_menstrual_period'
                THEN jsonb_set(
                    field_rule,
                    '{parameters}',
                    '{"date_format":"YYYY-MM-DD","allow_empty":true,"allow_unknown":true}'::jsonb
                )
            WHEN field_rule->>'code' = 'value_and_unit' AND field.code = 'pediatric_weight'
                THEN jsonb_set(
                    jsonb_set(
                        field_rule,
                        '{parameters}',
                        '{"units":["kg","公斤","千克"],"allow_empty":true,"non_negative":true}'::jsonb
                    ),
                    '{llm_instruction}',
                    '"体重必须填写数值和千克单位；允许kg、公斤、千克，不允许使用斤。"'::jsonb
                )
            ELSE field_rule
        END
    )
    FROM jsonb_array_elements(field.quality_rules) AS rule_item(field_rule)
)
FROM emr_fields AS field
WHERE target.id = field.id
  AND field.code IN ('followup', 'last_menstrual_period', 'pediatric_weight');

-- +goose Down
