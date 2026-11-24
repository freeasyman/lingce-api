-- 生产收敛基线 20261123_001
-- 介入信号已读状态：菜单角标只统计未读待处理。
ALTER TABLE deal_intervention_signals
    ADD COLUMN IF NOT EXISTS viewed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS viewed_by BIGINT;

CREATE INDEX IF NOT EXISTS idx_deal_intervention_signals_unread
    ON deal_intervention_signals (tenant_id)
    WHERE status = 'pending' AND viewed_at IS NULL;
