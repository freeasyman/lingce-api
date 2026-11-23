-- 待我处理角标改为「未读数」：详情打开即标记已读，角标只统计未读的待处理信号。
ALTER TABLE deal_intervention_signals
    ADD COLUMN IF NOT EXISTS viewed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS viewed_by BIGINT;

-- 角标查询：按租户统计未读待处理（部分索引，只覆盖未读行）。
CREATE INDEX IF NOT EXISTS idx_deal_intervention_signals_unread
    ON deal_intervention_signals (tenant_id)
    WHERE status = 'pending' AND viewed_at IS NULL;
