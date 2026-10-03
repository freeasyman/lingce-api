-- 当前质检投影按医生展示粒度去重。
--
-- 原始规则结果仍保留在 emr_check_results；这里只清理当前投影中的重复展示项。
-- 同一病历、同一栏位、同一展示文案只保留一条，阻断项优先保留。

WITH ranked AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY
                record_id,
                COALESCE(NULLIF(field_code, ''), NULLIF(rule_code, '')),
                display_message
            ORDER BY
                is_blocking DESC,
                is_attention DESC,
                checked_at DESC,
                sort_order,
                id
        ) AS row_number
    FROM public.emr_record_quality_current_items
)
DELETE FROM public.emr_record_quality_current_items item
USING ranked
WHERE item.id = ranked.id
  AND ranked.row_number > 1;

UPDATE public.emr_record_quality_current current_quality
SET
    result_count = counts.result_count,
    attention_count = counts.attention_count,
    blocking_count = counts.blocking_count,
    updated_at = NOW()
FROM (
    SELECT
        record_id,
        COUNT(*)::integer AS result_count,
        COUNT(*) FILTER (WHERE is_attention)::integer AS attention_count,
        COUNT(*) FILTER (WHERE is_blocking)::integer AS blocking_count
    FROM public.emr_record_quality_current_items
    GROUP BY record_id
) counts
WHERE current_quality.record_id = counts.record_id;

UPDATE public.emr_record_quality_current
SET
    result_count = 0,
    attention_count = 0,
    blocking_count = 0,
    updated_at = NOW()
WHERE NOT EXISTS (
    SELECT 1
    FROM public.emr_record_quality_current_items item
    WHERE item.record_id = emr_record_quality_current.record_id
);
