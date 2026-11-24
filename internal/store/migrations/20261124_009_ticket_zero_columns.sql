-- 工单流程统一用已有列：status 一列表达状态，resolution_type 表达处理方式，
-- executed_at 表达处理时间，"已发送给客户" 记在 extra_data，不再新增列。

-- 1) 处理方式写回已有列（历史 our_issue 视为换新）
UPDATE badge_tickets
SET resolution_type = CASE
    WHEN resolution IS NOT NULL THEN resolution
    WHEN resolution_type = 'our_issue' THEN 'swap'
    WHEN resolution_type = 'customer_issue' THEN 'none'
    WHEN resolution_type = 'other' THEN 'none'
    ELSE resolution_type
END
WHERE resolution IS NOT NULL OR resolution_type IN ('our_issue', 'customer_issue', 'other');

-- 2) 处理时间复用 executed_at
UPDATE badge_tickets
SET executed_at = COALESCE(executed_at, resolved_at)
WHERE resolved_at IS NOT NULL;

-- 3) 客户已发送留痕写进 extra_data
UPDATE badge_tickets
SET extra_data = COALESCE(extra_data, '{}'::jsonb) || jsonb_build_object(
    'customer_notified_at', to_char(customer_notified_at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD"T"HH24:MI:SS'),
    'customer_notified_by', customer_notified_by
)
WHERE customer_notified_at IS NOT NULL;

-- 4) 删掉 008 新增的 4 列
ALTER TABLE badge_tickets
    DROP COLUMN IF EXISTS resolution,
    DROP COLUMN IF EXISTS resolved_at,
    DROP COLUMN IF EXISTS customer_notified_at,
    DROP COLUMN IF EXISTS customer_notified_by;
